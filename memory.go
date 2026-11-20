package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Fact struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type memoryFile struct {
	Facts []Fact `json:"facts"`
}

type stateFile struct {
	Name      string `json:"name"`
	Affection int    `json:"affection"`
	Sessions  int    `json:"sessions"`
}

// Store persists memory.json and state.json under ~/.nyaterm.
type Store struct {
	mu    sync.Mutex
	dir   string
	mem   memoryFile
	state stateFile
}

func LoadStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".nyaterm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, state: stateFile{Name: "senpai", Affection: 40}}
	_ = readJSON(filepath.Join(dir, "memory.json"), &s.mem)
	_ = readJSON(filepath.Join(dir, "state.json"), &s.state)
	return s, nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// save must be called with s.mu held.
func (s *Store) save() error {
	if err := writeJSON(filepath.Join(s.dir, "memory.json"), s.mem); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.dir, "state.json"), s.state)
}

func (s *Store) SaveFact(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("fact is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range s.mem.Facts {
		if strings.EqualFold(f.Text, text) {
			return nil // already remembered
		}
	}
	s.mem.Facts = append(s.mem.Facts, Fact{Text: text, At: time.Now()})
	return s.save()
}

func (s *Store) Facts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.mem.Facts))
	for _, f := range s.mem.Facts {
		out = append(out, f.Text)
	}
	return out
}

func (s *Store) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Name
}

func (s *Store) SetName(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Name = name
	return s.save()
}

func (s *Store) Affection() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Affection
}

func (s *Store) AddAffection(delta int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Affection = clamp(s.state.Affection+delta, 0, 100)
	_ = s.save()
}

func (s *Store) BumpSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Sessions++
	_ = s.save()
}

func (s *Store) Sessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Sessions
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// MoodFor maps affection to one of four moods.
func MoodFor(a int) string {
	switch {
	case a < 30:
		return "Annoyed"
	case a < 60:
		return "Flustered"
	case a < 85:
		return "Secretly Happy"
	default:
		return "Dere Mode"
	}
}

// MoodFace is the cat-ear status indicator shown in the header.
func MoodFace(a int) string {
	switch MoodFor(a) {
	case "Annoyed":
		return "ᓚᘏᗢ 💢"
	case "Flustered":
		return "ᓚᘏᗢ 🙈"
	case "Secretly Happy":
		return "ᓚᘏᗢ 💕"
	default:
		return "ᓚᘏᗢ 💖"
	}
}
