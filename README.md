# nyaterm 🐾

A tsundere catgirl AI companion and PC/coding assistant for your terminal.
Powered by Gemini (free tier) or a local model through Ollama.

## Features
- Agentic tool loop: read/write/edit files, run shell commands, system info, open apps, clipboard
- Persistent memory in `~/.nyaterm/memory.json`
- Affection meter and moods in `~/.nyaterm/state.json`
- Consent prompts before any write, edit, shell command, or open action
- `--yolo` auto-approves file writes and edits (shell and open still ask)

## 1. Initialize the repository

```bash
cd nyaterm
go mod tidy
git init
git add .
git commit -m "Initial commit: nyaterm"
git branch -M main
git remote add origin https://github.com/Skydorino/nyaterm.git
git push -u origin main
```

Replace `yourname` in `go.mod`, `.goreleaser.yaml`, and the import paths with your GitHub username.

## 2. Choose a model

nyaterm works with two free options. Pick one.

### Option A: Gemini (better answers, needs internet)

Your messages and any file or command output the AI reads are sent to Google.

1. Go to **aistudio.google.com**, sign in, and click **Get API key**. No payment method is needed.
2. Set the key before launching:

macOS / Linux:
```bash
export GEMINI_API_KEY="your-key"
```

Windows (PowerShell):
```powershell
$env:GEMINI_API_KEY = "your-key"
```

### Option B: Ollama (runs fully on your PC, no key, no limits)

Use this if you want everything local. It works on modest hardware, though replies are slower and weaker than Gemini.

1. Install Ollama from ollama.com, then download a model. For a 16 GB PC with no dedicated GPU, use `qwen2.5-coder:3b`. If replies are too slow, use `qwen2.5-coder:1.5b`.

```bash
ollama pull qwen2.5-coder:3b
```

2. Set these before launching:

macOS / Linux:
```bash
export NYATERM_BASE_URL="http://localhost:11434/v1"
export NYATERM_API_KEY="ollama"
export NYATERM_MODEL="qwen2.5-coder:3b"
```

Windows (PowerShell):
```powershell
$env:NYATERM_BASE_URL = "http://localhost:11434/v1"
$env:NYATERM_API_KEY = "ollama"
$env:NYATERM_MODEL = "qwen2.5-coder:3b"
```

The Ollama key can be any text, since Ollama doesn't check it.

### Other settings

- `NYATERM_MODEL` picks the model. Without it, Gemini uses `gemini-2.5-flash`.
- `NYATERM_BASE_URL` and `NYATERM_API_KEY` let you point nyaterm at any OpenAI-compatible provider.

## 3. Build and run locally

```bash
go build -o nyaterm .
./nyaterm            # or ./nyaterm --yolo (use .\\nyaterm.exe on Windows)
```

## 4. Trigger a GitHub Release build

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions compiles Windows, macOS, and Linux binaries (amd64 and arm64) and attaches them to a new Release.

## Slash commands
| Command | Action |
|---|---|
| `/stats` | Mood, affection bar, memory count, sessions |
| `/clear` | Reset the chat and conversation history |
| `/rename <name>` | Change what she calls you |
| `/exit` | Quit |

## Keys
- `Enter` send · `PgUp`/`PgDn` scroll · `y`/`n` answer approval prompts · `Ctrl+C` quit
