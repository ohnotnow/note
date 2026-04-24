package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type note struct {
	path    string
	mtime   time.Time
	content string
}

type store struct {
	dir string
}

func notesDir() (string, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfg, "note", "notes"), nil
}

func (s *store) loadAll() ([]note, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	notes := make([]note, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		full := filepath.Join(s.dir, e.Name())
		b, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		notes = append(notes, note{
			path:    full,
			mtime:   info.ModTime(),
			content: string(b),
		})
	}
	sort.Slice(notes, func(i, j int) bool {
		return notes[i].mtime.After(notes[j].mtime)
	})
	return notes, nil
}

func (s *store) newPath() string {
	return filepath.Join(s.dir, fmt.Sprintf("note-%s.md", time.Now().Format("20060102-150405")))
}

func (s *store) writeNew(content string) (string, error) {
	path := s.newPath()
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func firstMeaningfulLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "#")
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "(empty note)"
}

func expandPath(p string) string {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
