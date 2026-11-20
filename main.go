package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

func main() {
	yolo := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--yolo":
			yolo = true
		case "--version":
			fmt.Println("nyaterm", version)
			return
		case "-h", "--help":
			fmt.Println("usage: nyaterm [--yolo] [--version]")
			fmt.Println("  --yolo  auto-approve file writes/edits (shell & open still ask)")
			return
		}
	}

	if apiKey() == "" {
		fmt.Fprintln(os.Stderr, "nyaa~ I need an API key to think! Set GEMINI_API_KEY (free at aistudio.google.com) and try again, baka!")
		os.Exit(1)
	}

	store, err := LoadStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "couldn't open ~/.nyaterm:", err)
		os.Exit(1)
	}
	store.BumpSession()

	events := make(chan Event, 64)
	agent := NewAgent(store, yolo, events)
	p := tea.NewProgram(newModel(store, agent, events, yolo), tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "nyaa, something exploded:", err)
		os.Exit(1)
	}
}
