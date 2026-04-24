package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	dir, err := notesDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	s := &store{dir: dir}

	if len(os.Args) > 1 {
		content := strings.Join(os.Args[1:], " ")
		path, err := s.writeNew(content)
		if err != nil {
			return err
		}
		fmt.Println("saved:", path)
		return nil
	}

	notes, err := s.loadAll()
	if err != nil {
		return err
	}
	return runTUI(s, notes)
}
