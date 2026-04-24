package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// renderCache is keyed on path+mtime+width. Bounded-ish: one entry per
// (note, viewport width) combination, which in normal use is a handful.
var renderCache = map[string]string{}

func renderMarkdown(n note, width int) string {
	if width < 20 {
		width = 20
	}
	key := fmt.Sprintf("%s|%d|%d", n.path, n.mtime.UnixNano(), width)
	if s, ok := renderCache[key]; ok {
		return s
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dracula"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return n.content
	}
	out, err := r.Render(n.content)
	if err != nil {
		return n.content
	}
	out = strings.TrimRight(out, "\n")
	renderCache[key] = out
	return out
}

type mode int

const (
	modeList mode = iota
	modeSearch
	modeExpanded
	modeExport
	modeConfirmDelete
)

var (
	styleTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	styleSubtle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleSelected = lipgloss.NewStyle().Background(lipgloss.Color("237")).Foreground(lipgloss.Color("229"))
	styleStatus   = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	styleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleDivider  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

type model struct {
	store          *store
	notes          []note
	filtered       []int
	cursor         int
	search         string
	export         string
	mode           mode
	status         string
	statusErr      bool
	width          int
	height         int
	expandedScroll int
}

type editorFinishedMsg struct{ err error }
type notesReloadedMsg struct {
	notes []note
	err   error
}

func runTUI(s *store, notes []note) error {
	m := model{store: s, notes: notes}
	m.applyFilter()
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	return err
}

func (m *model) applyFilter() {
	q := strings.ToLower(m.search)
	m.filtered = m.filtered[:0]
	for i, n := range m.notes {
		if q == "" || strings.Contains(strings.ToLower(n.content), q) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m model) selected() (note, bool) {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return note{}, false
	}
	return m.notes[m.filtered[m.cursor]], true
}

func (m *model) setStatus(msg string, isErr bool) {
	m.status = msg
	m.statusErr = isErr
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case editorFinishedMsg:
		m.cursor = 0
		return m, reloadNotesCmd(m.store)
	case notesReloadedMsg:
		if msg.err != nil {
			m.setStatus("reload failed: "+msg.err.Error(), true)
			return m, nil
		}
		m.notes = msg.notes
		m.applyFilter()
		if m.cursor >= len(m.filtered) {
			m.cursor = len(m.filtered) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		return m, nil
	case tea.KeyMsg:
		// any key clears transient status
		m.status = ""
		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeSearch:
			return m.updateSearch(msg)
		case modeExpanded:
			return m.updateExpanded(msg)
		case modeExport:
			return m.updateExport(msg)
		case modeConfirmDelete:
			return m.updateConfirmDelete(msg)
		}
	}
	return m, nil
}

func reloadNotesCmd(s *store) tea.Cmd {
	return func() tea.Msg {
		ns, err := s.loadAll()
		return notesReloadedMsg{notes: ns, err: err}
	}
}

func openEditorCmd(path string) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	c := exec.Command(editor, path)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinishedMsg{err: err}
	})
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j", "ctrl+n":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		if len(m.filtered) > 0 {
			m.cursor = len(m.filtered) - 1
		}
	case "enter", " ":
		if _, ok := m.selected(); ok {
			m.mode = modeExpanded
			m.expandedScroll = 0
		}
	case "c":
		if n, ok := m.selected(); ok {
			if err := clipboard.WriteAll(n.content); err != nil {
				m.setStatus("copy failed: "+err.Error(), true)
			} else {
				m.setStatus("copied to clipboard", false)
			}
		}
	case "e":
		if _, ok := m.selected(); ok {
			m.mode = modeExport
			m.export = ""
		}
	case "n":
		return m, openEditorCmd(m.store.newPath())
	case "d", "backspace":
		if _, ok := m.selected(); ok {
			m.mode = modeConfirmDelete
		}
	case "/":
		m.mode = modeSearch
	case "esc":
		if m.search != "" {
			m.search = ""
			m.applyFilter()
		} else {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = modeList
		return m, nil
	case tea.KeyEnter:
		m.mode = modeList
		return m, nil
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyBackspace:
		if len(m.search) > 0 {
			rs := []rune(m.search)
			m.search = string(rs[:len(rs)-1])
			m.applyFilter()
		}
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
	case tea.KeyDown:
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case tea.KeyRunes, tea.KeySpace:
		m.search += msg.String()
		m.applyFilter()
	}
	return m, nil
}

func (m model) updateExpanded(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.mode = modeList
		m.expandedScroll = 0
	case "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.expandedScroll > 0 {
			m.expandedScroll--
		}
	case "down", "j":
		m.expandedScroll++
	case "pgup":
		m.expandedScroll -= m.height / 2
		if m.expandedScroll < 0 {
			m.expandedScroll = 0
		}
	case "pgdown", " ":
		m.expandedScroll += m.height / 2
	case "g", "home":
		m.expandedScroll = 0
	case "c":
		if n, ok := m.selected(); ok {
			if err := clipboard.WriteAll(n.content); err != nil {
				m.setStatus("copy failed: "+err.Error(), true)
			} else {
				m.setStatus("copied to clipboard", false)
			}
		}
	}
	return m, nil
}

func (m model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "n", "N", "esc":
		m.mode = modeList
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "y", "Y", "enter", " ":
		n, ok := m.selected()
		if !ok {
			m.mode = modeList
			return m, nil
		}
		if err := os.Remove(n.path); err != nil {
			m.setStatus("delete failed: "+err.Error(), true)
			m.mode = modeList
			return m, nil
		}
		m.setStatus("deleted "+filepath.Base(n.path), false)
		m.mode = modeList
		return m, reloadNotesCmd(m.store)
	}
	return m, nil
}

func (m model) updateExport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.mode = modeList
		m.export = ""
	case tea.KeyEnter:
		n, ok := m.selected()
		if !ok {
			m.mode = modeList
			return m, nil
		}
		name := strings.TrimSpace(m.export)
		if name == "" {
			m.setStatus("cancelled: no filename", true)
			m.mode = modeList
			m.export = ""
			return m, nil
		}
		path := expandPath(name)
		if err := os.WriteFile(path, []byte(n.content), 0o644); err != nil {
			m.setStatus("export failed: "+err.Error(), true)
		} else {
			m.setStatus("exported to "+path, false)
		}
		m.mode = modeList
		m.export = ""
	case tea.KeyBackspace:
		if len(m.export) > 0 {
			rs := []rune(m.export)
			m.export = string(rs[:len(rs)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		m.export += msg.String()
	}
	return m, nil
}

// ---------- view ----------

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}
	if m.mode == modeExpanded {
		return m.viewExpanded()
	}
	return m.viewList()
}

func (m model) viewList() string {
	if len(m.notes) == 0 {
		msg := styleSubtle.Render("no notes yet — press ") + styleTitle.Render("n") + styleSubtle.Render(" to create one, ") + styleTitle.Render("q") + styleSubtle.Render(" to quit")
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
	}

	leftWidth := m.width / 3
	if leftWidth < 28 {
		leftWidth = 28
	}
	if leftWidth > 55 {
		leftWidth = 55
	}
	// layout: [left]  │ [right]
	const gutter = 4 // " " + "│" + " " + safety
	rightWidth := m.width - leftWidth - gutter
	if rightWidth < 10 {
		rightWidth = 10
	}
	bodyHeight := m.height - 2
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	// scroll so cursor stays visible
	scroll := 0
	if m.cursor >= bodyHeight {
		scroll = m.cursor - bodyHeight + 1
	}

	// LEFT column — each row pre-padded to leftWidth visible chars.
	leftLines := make([]string, 0, bodyHeight)
	for i := scroll; i < len(m.filtered) && len(leftLines) < bodyHeight; i++ {
		n := m.notes[m.filtered[i]]
		date := n.mtime.Format("02/01/06")
		label := truncate(firstMeaningfulLine(n.content), leftWidth-11)
		if i == m.cursor {
			raw := "▸ " + date + " " + label
			leftLines = append(leftLines, styleSelected.Render(padToWidth(raw, leftWidth)))
		} else {
			raw := "  " + styleSubtle.Render(date) + " " + label
			leftLines = append(leftLines, padToWidth(raw, leftWidth))
		}
	}
	for len(leftLines) < bodyHeight {
		leftLines = append(leftLines, strings.Repeat(" ", leftWidth))
	}

	// RIGHT column — glamour-rendered markdown, trust its word-wrap.
	rightLines := make([]string, 0, bodyHeight)
	if n, ok := m.selected(); ok {
		rightLines = append(rightLines, truncate(styleSubtle.Render(filepath.Base(n.path)), rightWidth))
		rendered := renderMarkdown(n, rightWidth-2)
		for _, line := range strings.Split(rendered, "\n") {
			if len(rightLines) >= bodyHeight {
				break
			}
			rightLines = append(rightLines, line)
		}
	}
	for len(rightLines) < bodyHeight {
		rightLines = append(rightLines, "")
	}

	// DIVIDER column — one "│" per row.
	divLines := make([]string, bodyHeight)
	bar := styleDivider.Render("│")
	for i := range divLines {
		divLines[i] = bar
	}

	top := lipgloss.JoinHorizontal(
		lipgloss.Top,
		strings.Join(leftLines, "\n"),
		" "+strings.Join(divLines, "\n "), // leading space on each row
		" "+strings.Join(rightLines, "\n "),
	)
	return top + "\n" + m.bottomBar(leftWidth)
}

func padToWidth(s string, w int) string {
	vw := lipgloss.Width(s)
	if vw >= w {
		return s
	}
	return s + strings.Repeat(" ", w-vw)
}

func (m model) viewExpanded() string {
	n, ok := m.selected()
	if !ok {
		m2 := m
		m2.mode = modeList
		return m2.viewList()
	}
	bodyHeight := m.height - 2
	header := styleTitle.Render(filepath.Base(n.path))
	rendered := renderMarkdown(n, m.width-2)
	lines := strings.Split(rendered, "\n")

	if m.expandedScroll > len(lines)-1 {
		m.expandedScroll = maxInt(0, len(lines)-1)
	}
	var body strings.Builder
	rows := 0
	for i := m.expandedScroll; i < len(lines) && rows < bodyHeight-1; i++ {
		body.WriteString(truncate(lines[i], m.width) + "\n")
		rows++
	}

	var bar string
	if m.status != "" {
		if m.statusErr {
			bar = styleError.Render(m.status)
		} else {
			bar = styleStatus.Render(m.status)
		}
	} else {
		hint := "q/esc back · c copy · ↑↓ scroll"
		if len(lines) > bodyHeight-1 {
			hint = fmt.Sprintf("line %d/%d · ", m.expandedScroll+1, len(lines)) + hint
		}
		bar = styleSubtle.Render(hint)
	}

	return header + "\n" + body.String() + "\n" + bar
}

func (m model) bottomBar(leftWidth int) string {
	switch m.mode {
	case modeSearch:
		return styleTitle.Render("/ ") + m.search + "▌\n" +
			styleSubtle.Render("enter/esc finish · ↑↓ nav")
	case modeExport:
		return styleTitle.Render("export to: ") + m.export + "▌\n" +
			styleSubtle.Render("enter save · esc cancel · ~ and relative paths ok")
	case modeConfirmDelete:
		preview := "(note)"
		if n, ok := m.selected(); ok {
			preview = truncate(firstMeaningfulLine(n.content), 40)
		}
		return styleError.Render("delete ") + "\"" + preview + "\"" + styleError.Render("?") +
			styleSubtle.Render("  [") + styleTitle.Render("Y") + styleSubtle.Render("/n]") + "\n" +
			styleSubtle.Render("enter/y confirm · n/esc cancel")
	}

	var count string
	if len(m.filtered) == 0 {
		count = styleSubtle.Render(fmt.Sprintf("0 of %d", len(m.notes)))
	} else if m.search != "" {
		count = styleSubtle.Render(fmt.Sprintf("%d of %d match", m.cursor+1, len(m.filtered)))
	} else {
		count = styleSubtle.Render(fmt.Sprintf("%d of %d", m.cursor+1, len(m.notes)))
	}
	var line1 string
	switch {
	case m.status != "" && m.statusErr:
		line1 = styleError.Render(m.status)
	case m.status != "":
		line1 = styleStatus.Render(m.status)
	case m.search != "":
		line1 = styleSubtle.Render(fmt.Sprintf("filter: %q (esc clear)", m.search)) + "  " + count
	default:
		line1 = count
	}
	line2 := styleSubtle.Render("↑↓ nav · enter preview · n new · c copy · e export · d delete · / search · q quit")
	return line1 + "\n" + line2
}

// ---------- helpers ----------

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\t", "    ")
	rs := []rune(s)
	if max <= 0 {
		return ""
	}
	if len(rs) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(rs[:max-1]) + "…"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
