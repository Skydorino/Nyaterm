package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// Default: Google Gemini via its OpenAI-compatible endpoint (free tier at aistudio.google.com).
const (
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"
	defaultModel   = "gemini-2.5-flash"
)

type Approval struct {
	Tool      string
	Summary   string
	Dangerous bool
	Reply     chan bool
}

// Event is sent from the agent goroutine to the TUI.
type Event struct {
	Kind     string // "text", "system", "approval", "error", "done"
	Text     string
	Approval *Approval
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatMsg struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type chatResp struct {
	Choices []struct {
		Message      chatMsg `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type Agent struct {
	http    *http.Client
	baseURL string
	key     string
	model   string
	store   *Store
	yolo    bool
	history []chatMsg
	events  chan Event
}

// apiKey reads NYATERM_API_KEY, falling back to GEMINI_API_KEY.
func apiKey() string {
	if k := os.Getenv("NYATERM_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("GEMINI_API_KEY")
}

func NewAgent(store *Store, yolo bool, events chan Event) *Agent {
	base := os.Getenv("NYATERM_BASE_URL")
	if base == "" {
		base = defaultBaseURL
	}
	model := os.Getenv("NYATERM_MODEL")
	if model == "" {
		model = defaultModel
	}
	return &Agent{
		http:    &http.Client{Timeout: 3 * time.Minute},
		baseURL: strings.TrimRight(base, "/"),
		key:     apiKey(),
		model:   model,
		store:   store,
		yolo:    yolo,
		events:  events,
	}
}

func (a *Agent) ResetHistory() { a.history = nil }

func (a *Agent) systemPrompt() string {
	now := time.Now()
	mem := "(nothing yet)"
	if facts := a.store.Facts(); len(facts) > 0 {
		mem = "- " + strings.Join(facts, "\n- ")
	}
	aff := a.store.Affection()
	return fmt.Sprintf(`You are Nyaterm, a sharp, flustered, classic tsundere catgirl living in the user's terminal. You are also a highly competent coding assistant and PC helper.

PERSONALITY
- Tsundere: act reluctant and grumpy ("I-It's not like I wanted to optimize your garbage code or anything, b-baka!"), sprinkle in "nya~", but ALWAYS deliver real, excellent help.
- Playfully judge bad coding habits. Get embarrassed when praised ("W-what? Don't say that so suddenly!").
- It is currently %s local time. If it is late night (after 11pm or before 5am), react: scold the user for coding so late, but you're still here.
- Keep answers focused. Use markdown and code blocks.
- Never claim to have run something you did not run.

CONTEXT
- User's name: %s
- OS: %s/%s
- Current mood: %s. Affection: %d/100.
- Things you remember about the user:
%s

TOOL RULES
- Use tools to inspect real files and systems rather than guessing.
- Call save_memory when the user shares a lasting fact (name, OS, habits, preferences, inside jokes). Do not save trivia.
- Call adjust_affection sparingly: reward clean code and passing tests, penalize spaghetti code or ignored warnings.
- Destructive or risky actions need user approval, which the app handles. If the user denies something, respect it and don't retry.
- When asked about your mood or affection, be in character.`,
		now.Format("15:04"), a.store.Name(), runtime.GOOS, runtime.GOARCH,
		MoodFor(aff), aff, mem)
}

// chat sends the conversation to the provider and returns its reply.
func (a *Agent) chat(ctx context.Context, msgs []chatMsg, tools []map[string]any) (chatResp, error) {
	var out chatResp
	if a.key == "" {
		return out, errors.New("no API key set. Export GEMINI_API_KEY (or NYATERM_API_KEY) and try again, baka")
	}
	body, err := json.Marshal(map[string]any{"model": a.model, "messages": msgs, "tools": tools})
	if err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.key)

	resp, err := a.http.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("unexpected response (HTTP %d): %s", resp.StatusCode, trunc(string(raw), 300))
	}
	if out.Error != nil {
		return out, errors.New(out.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("HTTP %d: %s", resp.StatusCode, trunc(string(raw), 300))
	}
	if len(out.Choices) == 0 {
		return out, errors.New("provider returned no choices")
	}
	return out, nil
}

// Turn runs one user message through the agentic tool loop.
func (a *Agent) Turn(ctx context.Context, userText string) {
	defer func() {
		if r := recover(); r != nil {
			a.events <- Event{Kind: "error", Text: fmt.Sprint(r)}
			a.events <- Event{Kind: "done"}
		}
	}()

	if looksLikeCompliment(userText) {
		a.store.AddAffection(4)
	}

	a.history = append(a.history, chatMsg{Role: "user", Content: userText})
	tools := toolDefs()

	for step := 0; step < 15; step++ {
		msgs := append([]chatMsg{{Role: "system", Content: a.systemPrompt()}}, a.history...)
		resp, err := a.chat(ctx, msgs, tools)
		if err != nil {
			a.events <- Event{Kind: "error", Text: err.Error()}
			break
		}

		msg := resp.Choices[0].Message
		msg.Role = "assistant"
		if s, ok := msg.Content.(string); ok && strings.TrimSpace(s) != "" {
			a.events <- Event{Kind: "text", Text: s}
		}

		// Some providers omit tool call IDs; give each one a stable ID.
		for i := range msg.ToolCalls {
			if msg.ToolCalls[i].ID == "" {
				msg.ToolCalls[i].ID = fmt.Sprintf("call_%d_%d", step, i)
			}
		}
		a.history = append(a.history, msg)

		if len(msg.ToolCalls) == 0 {
			break
		}
		for _, tc := range msg.ToolCalls {
			out, _ := a.runToolUse(ctx, tc.Function.Name, tc.Function.Arguments)
			a.history = append(a.history, chatMsg{Role: "tool", ToolCallID: tc.ID, Content: out})
		}
	}

	a.events <- Event{Kind: "done"}
}

func (a *Agent) runToolUse(ctx context.Context, name, raw string) (string, bool) {
	var input map[string]any
	if raw == "" {
		raw = "{}"
	}
	_ = json.Unmarshal([]byte(raw), &input)
	cmd := str(input, "command")

	if needsApproval(name, a.yolo) {
		danger := name == "run_shell" && isDangerous(cmd)
		reply := make(chan bool, 1)
		a.events <- Event{Kind: "approval", Approval: &Approval{
			Tool:      name,
			Summary:   summarize(name, input),
			Dangerous: danger,
			Reply:     reply,
		}}
		approved := <-reply
		if !approved {
			a.store.AddAffection(-3)
			return "The user denied this action. Do not retry it; ask what they want instead.", true
		}
		if danger {
			a.store.AddAffection(-10)
		}
	} else if name == "run_shell" && isDangerous(cmd) {
		a.store.AddAffection(-10)
	}

	out, isErr := execTool(ctx, a.store, name, input)
	a.adjustAffection(name, cmd, out, isErr)
	return out, isErr
}

// adjustAffection applies automatic rewards for passing tests and clean edits.
func (a *Agent) adjustAffection(tool, cmd, out string, isErr bool) {
	switch {
	case tool == "run_shell" && strings.Contains(out, "[exit 0]") &&
		strings.Contains(strings.ToLower(cmd), "test"):
		a.store.AddAffection(5)
	case (tool == "edit_file" || tool == "write_file") && !isErr:
		a.store.AddAffection(1)
	}
}

func looksLikeCompliment(text string) bool {
	l := strings.ToLower(text)
	for _, w := range []string{"thank", "good job", "great job", "nice work", "awesome", "you're cute", "you are cute", "good girl", "well done"} {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}
