package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

var (
	pink     = lipgloss.Color("#FFB7D5")
	lavender = lipgloss.Color("#C8A2FF")
	magenta  = lipgloss.Color("#FF6EC7")
	muted    = lipgloss.Color("#8A7A9B")
	errColor = lipgloss.Color("#FF4D6D")

	headerSt = lipgloss.NewStyle().Background(lipgloss.Color("#3B2340")).Foreground(pink).Padding(0, 1)
	userSt   = lipgloss.NewStyle().Foreground(pink).Bold(true)
	catSt    = lipgloss.NewStyle().Foreground(magenta).Bold(true)
	sysSt    = lipgloss.NewStyle().Foreground(muted).Italic(true)
	errSt    = lipgloss.NewStyle().Foreground(errColor).Bold(true)
	promptSt = lipgloss.NewStyle().Foreground(lavender).Bold(true)
	spinSt   = lipgloss.NewStyle().Foreground(lavender)
	boxSt    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(magenta).Padding(0, 1)

	spinnerLines = []string{
		"twitching ears while parsing your stack trace...",
		"pretending I'm not reading your code...",
		"warming up my claws... don't stare!",
		"sniffing around your repo, nya~",
	}
)

type eventMsg Event
type tickMsg time.Time

type model struct {
	input    textinput.Model
	vp       viewport.Model
	agent    *Agent
	store    *Store
	events   chan Event
	renderer *glamour.TermRenderer
	lines    []string
	busy     bool
	pending  *Approval
	spin     int
	width    int
	height   int
	yolo     bool
}

func newModel(store *Store, agent *Agent, events chan Event, yolo bool) model {
	ti := textinput.New()
	ti.Placeholder = "say something, baka... (/stats /clear /rename /exit)"
	ti.Prompt = "🐾 "
	ti.PromptStyle = promptSt
	ti.Focus()

	r, _ := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(80))
	m := model{
		input:    ti,
		vp:       viewport.New(80, 20),
		agent:    agent,
		store:    store,
		events:   events,
		renderer: r,
		yolo:     yolo,
	}
	m.lines = []string{catSt.Render("nyaterm ▸") + "\n" + sysSt.Render(greeting(store.Name()))}
	m.refresh()
	return m
}

func greeting(name string) string {
	h := time.Now().Hour()
	if h >= 23 || h < 5 {
		return fmt.Sprintf("It's this late and you're still awake, %s?! Don't blame me when you're tired tomorrow... I-I'm only here because I'm worried, nya~", name)
	}
	return fmt.Sprintf("H-hmph. You're back, %s? I wasn't waiting or anything. What do you need? Nya~", name)
}

func tickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func waitEvent(ch chan Event) tea.Cmd {
	return func() tea.Msg { return eventMsg(<-ch) }
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, waitEvent(m.events), tickCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.Width = msg.Width
		m.vp.Height = max(msg.Height-8, 3)
		m.input.Width = max(msg.Width-6, 10)
		m.renderer, _ = glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(max(msg.Width-6, 20)))
		m.refresh()
		return m, nil
	case tickMsg:
		m.spin++
		return m, tickCmd()
	case eventMsg:
		m.handleEvent(Event(msg))
		return m, waitEvent(m.events)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	// While an approval is pending, only y/n is accepted.
	if m.pending != nil {
		switch msg.String() {
		case "y", "Y":
			m.resolve(true)
		case "n", "N", "enter", "esc":
			m.resolve(false)
		}
		m.refresh()
		return m, nil
	}

	switch msg.Type {
	case tea.KeyEnter:
		if m.busy {
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		m.input.SetValue("")
		if text == "" {
			return m, nil
		}
		cmd := m.submit(text)
		m.refresh()
		return m, cmd
	case tea.KeyPgUp, tea.KeyPgDown:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *model) submit(text string) tea.Cmd {
	if strings.HasPrefix(text, "/") {
		return m.slash(text)
	}
	m.lines = append(m.lines, userSt.Render("you ▸ ")+text)
	m.busy = true
	go m.agent.Turn(context.Background(), text)
	return nil
}

func (m *model) slash(text string) tea.Cmd {
	parts := strings.Fields(text)
	switch parts[0] {
	case "/exit", "/quit":
		return tea.Quit
	case "/clear":
		m.agent.ResetHistory()
		m.lines = []string{catSt.Render("nyaterm ▸") + "\n" + sysSt.Render("Fresh start. Don't think this means I forgot anything, nya.")}
	case "/rename":
		if len(parts) < 2 {
			m.addSys("Rename me to what? Use /rename <name>, baka.")
			break
		}
		name := strings.Join(parts[1:], " ")
		_ = m.store.SetName(name)
		m.addSys(fmt.Sprintf("F-fine, %s it is... don't get used to it.", name))
	case "/stats":
		m.addSys(statsBlock(m.store))
	default:
		m.addSys("That's not a command, b-baka. Try /stats /clear /rename /exit")
	}
	m.refresh()
	return nil
}

func statsBlock(s *Store) string {
	a := s.Affection()
	return fmt.Sprintf("Mood: %s %s\nAffection: %s %d/100\nMemories: %d saved\nSessions: %d",
		MoodFace(a), MoodFor(a), affectionBar(a), a, len(s.Facts()), s.Sessions())
}

func (m *model) handleEvent(ev Event) {
	switch ev.Kind {
	case "text":
		out := ev.Text
		if m.renderer != nil {
			if rendered, err := m.renderer.Render(ev.Text); err == nil {
				out = rendered
			}
		}
		m.lines = append(m.lines, catSt.Render("nyaterm ▸")+"\n"+strings.TrimRight(out, "\n"))
	case "system":
		m.addSys(ev.Text)
	case "approval":
		m.pending = ev.Approval
	case "error":
		m.lines = append(m.lines, errSt.Render("nyaa~ something broke: "+ev.Text))
	case "done":
		m.busy = false
	}
	m.refresh()
}

func (m *model) resolve(ok bool) {
	ap := m.pending
	m.pending = nil
	ap.Reply <- ok
	if ok {
		m.addSys("F-fine, I'll allow it. Don't make me regret it.")
	} else {
		m.addSys("Hmph. Skipped. Your loss, baka.")
	}
}

func (m *model) addSys(text string) {
	m.lines = append(m.lines, sysSt.Render(text))
}

func (m *model) refresh() {
	m.vp.SetContent(strings.Join(m.lines, "\n\n"))
	m.vp.GotoBottom()
}

func affectionBar(a int) string {
	filled := a / 10
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", 10-filled) + "]"
}

func (m model) View() string {
	if m.width == 0 {
		return "loading..."
	}

	a := m.store.Affection()
	header := headerSt.Width(m.width).Render(
		fmt.Sprintf("%s nyaterm · %s · %s %d/100", MoodFace(a), MoodFor(a), affectionBar(a), a))

	status := ""
	if m.busy && m.pending == nil {
		frame := spinnerLines[(m.spin/12)%len(spinnerLines)]
		status = spinSt.Render("(=^･ω･^=) " + frame)
	}

	bottom := m.input.View()
	if m.pending != nil {
		bottom = m.approvalView()
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, m.vp.View(), status, bottom)
}

func (m model) approvalView() string {
	ap := m.pending
	var b strings.Builder
	b.WriteString(promptSt.Render(fmt.Sprintf("🐾 Nyaa, the agent wants to use %s", ap.Tool)) + "\n")
	b.WriteString(ap.Summary + "\n")
	if ap.Dangerous {
		b.WriteString(errSt.Render("⚠ That looks DANGEROUS, baka! Are you sure about this?") + "\n")
	}
	b.WriteString(sysSt.Render("Allow it? [y/N]  (I-It's not like I care, nya~)"))
	return boxSt.Render(b.String())
}
