package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type mode int

const (
	modeList mode = iota
	modeSearch
	modeExpanded
	modeExport
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
		return m, reloadNotesCmd(m.store)
	case notesReloadedMsg:
		if msg.err != nil {
			m.setStatus("reload failed: "+msg.err.Error(), true)
		} else {
			m.notes = msg.notes
			m.cursor = 0
			m.applyFilter()
			m.setStatus(fmt.Sprintf("%d notes loaded", len(m.notes)), false)
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
	rightWidth := m.width - leftWidth - 3
	if rightWidth < 10 {
		rightWidth = 10
	}
	bodyHeight := m.height - 2 // reserve 2 lines for bottom bar
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	// left list, scroll so cursor stays visible
	scroll := 0
	if m.cursor >= bodyHeight {
		scroll = m.cursor - bodyHeight + 1
	}
	var left strings.Builder
	rows := 0
	for i := scroll; i < len(m.filtered) && rows < bodyHeight; i++ {
		n := m.notes[m.filtered[i]]
		date := n.mtime.Format("02/01/06")
		// 2 gutter + 9 date + 1 space = 12 chars before the label
		label := truncate(firstMeaningfulLine(n.content), leftWidth-12)
		if i == m.cursor {
			row := "▸ " + styleSubtle.Render(date) + " " + label
			left.WriteString(styleSelected.Width(leftWidth).Render(row))
		} else {
			left.WriteString("  " + styleSubtle.Render(date) + " " + label)
		}
		left.WriteString("\n")
		rows++
	}
	for rows < bodyHeight {
		left.WriteString("\n")
		rows++
	}

	// right preview
	var right strings.Builder
	if n, ok := m.selected(); ok {
		right.WriteString(styleSubtle.Render(filepath.Base(n.path)) + "\n")
		right.WriteString(styleDivider.Render(strings.Repeat("─", rightWidth)) + "\n")
		lines := strings.Split(n.content, "\n")
		remaining := bodyHeight - 2
		for _, line := range lines {
			if remaining <= 0 {
				break
			}
			right.WriteString(truncate(line, rightWidth) + "\n")
			remaining--
		}
	}

	leftBox := lipgloss.NewStyle().Width(leftWidth).Height(bodyHeight).Render(left.String())
	divider := lipgloss.NewStyle().Height(bodyHeight).Render(styleDivider.Render(strings.Repeat("│\n", bodyHeight)))
	rightBox := lipgloss.NewStyle().Width(rightWidth).Height(bodyHeight).PaddingLeft(1).Render(right.String())

	top := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, divider, rightBox)
	return top + "\n" + m.bottomBar(leftWidth)
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
	lines := strings.Split(n.content, "\n")

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
	}

	count := styleSubtle.Render(fmt.Sprintf("%d/%d", len(m.filtered), len(m.notes)))
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
	line2 := styleSubtle.Render("↑↓ nav · enter preview · n new · c copy · e export · / search · q quit")
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
