package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/lipgloss"
	"github.com/distatus/battery"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type props = map[string]any

func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

// tool builds one OpenAI-style function definition.
func tool(name, desc string, p props, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": desc,
			"parameters": map[string]any{
				"type":       "object",
				"properties": p,
				"required":   required,
			},
		},
	}
}

// toolDefs returns the tool definitions sent to the model.
func toolDefs() []map[string]any {
	return []map[string]any{
		tool("list_files", "List files and folders in a directory.",
			props{"dir": prop("string", "Directory path")}, "dir"),
		tool("read_file", "Read a text file.",
			props{"path": prop("string", "File path")}, "path"),
		tool("write_file", "Create or overwrite a file. Requires user approval.",
			props{"path": prop("string", "File path"), "content": prop("string", "Full file content")}, "path", "content"),
		tool("edit_file", "Replace exactly one occurrence of target with replacement in a file. Requires user approval.",
			props{
				"path":        prop("string", "File path"),
				"target":      prop("string", "Exact text to find (must occur once)"),
				"replacement": prop("string", "Text to replace it with"),
			}, "path", "target", "replacement"),
		tool("run_shell", "Run a shell command (bash/zsh, or PowerShell on Windows). Returns exit code, stdout and stderr. Requires user approval.",
			props{"command": prop("string", "Command to run")}, "command"),
		tool("system_info", "Report CPU, RAM, disk, battery and process counts.", props{}),
		tool("open_target", "Open a URL, file, or app with the OS default handler. Requires user approval.",
			props{"target": prop("string", "URL, file path or app name")}, "target"),
		tool("clipboard_tool", "Read from or write to the system clipboard.",
			props{
				"action": prop("string", "either 'read' or 'write'"),
				"text":   prop("string", "text to copy (for write)"),
			}, "action"),
		tool("save_memory", "Remember a lasting fact about the user (name, OS, coding habits, inside jokes).",
			props{"fact": prop("string", "A short, durable fact")}, "fact"),
		tool("recall_memories", "List everything remembered about the user.", props{}),
		tool("adjust_affection", "Nudge your own affection toward the user, between -8 and 8, for a real reason such as clean code, passing tests, spaghetti code, or ignored warnings. Use sparingly.",
			props{
				"delta":  prop("integer", "-8 to 8"),
				"reason": prop("string", "Short reason"),
			}, "delta", "reason"),
	}
}

func str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func num(m map[string]any, k string) int {
	v, _ := m[k].(float64)
	return int(v)
}

// needsApproval reports whether a tool must pause for user consent.
func needsApproval(tool string, yolo bool) bool {
	switch tool {
	case "write_file", "edit_file":
		return !yolo
	case "run_shell", "open_target":
		return true
	}
	return false
}

var dangerousPatterns = []string{
	"rm -rf", "rm -fr", "sudo rm", "mkfs", "dd if=", "shutdown", "reboot",
	"format c:", "del /s", "reg delete", ":(){", "> /dev/sd", "chmod -r 777 /",
}

func isDangerous(cmd string) bool {
	l := strings.ToLower(cmd)
	for _, p := range dangerousPatterns {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// summarize builds the approval prompt body shown to the user.
func summarize(tool string, in map[string]any) string {
	switch tool {
	case "write_file":
		c := str(in, "content")
		return fmt.Sprintf("write %s (%d bytes)", str(in, "path"), len(c))
	case "edit_file":
		return fmt.Sprintf("edit %s\n%s", str(in, "path"), renderDiff(str(in, "target"), str(in, "replacement")))
	case "run_shell":
		return "$ " + str(in, "command")
	case "open_target":
		return "open " + str(in, "target")
	}
	return tool
}

// renderDiff is a simple colored block diff: removed lines in pink, added lines in mint.
func renderDiff(oldText, newText string) string {
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B9A"))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color("#9BE9A8"))
	var b strings.Builder
	for _, l := range strings.Split(oldText, "\n") {
		b.WriteString(red.Render("- "+l) + "\n")
	}
	for _, l := range strings.Split(newText, "\n") {
		b.WriteString(green.Render("+ "+l) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// execTool runs a tool and returns its output and whether it failed.
func execTool(ctx context.Context, store *Store, name string, in map[string]any) (string, bool) {
	var out string
	var err error
	switch name {
	case "list_files":
		out, err = listFiles(str(in, "dir"))
	case "read_file":
		out, err = readFile(str(in, "path"))
	case "write_file":
		out, err = writeFile(str(in, "path"), str(in, "content"))
	case "edit_file":
		out, err = editFile(str(in, "path"), str(in, "target"), str(in, "replacement"))
	case "run_shell":
		out, err = runShell(ctx, str(in, "command"))
	case "system_info":
		out, err = systemInfo()
	case "open_target":
		out, err = openTarget(str(in, "target"))
	case "clipboard_tool":
		out, err = clipboardTool(str(in, "action"), str(in, "text"))
	case "save_memory":
		if err = store.SaveFact(str(in, "fact")); err == nil {
			out = "saved. I'll remember that... not that I was trying to, b-baka."
		}
	case "recall_memories":
		facts := store.Facts()
		if len(facts) == 0 {
			out = "no memories yet"
		} else {
			out = "- " + strings.Join(facts, "\n- ")
		}
	case "adjust_affection":
		d := clamp(num(in, "delta"), -8, 8)
		store.AddAffection(d)
		out = fmt.Sprintf("affection adjusted by %d (%s)", d, str(in, "reason"))
	default:
		err = fmt.Errorf("unknown tool %q", name)
	}
	if err != nil {
		return "error: " + err.Error(), true
	}
	return out, false
}

func listFiles(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, e := range entries {
		if i >= 300 {
			b.WriteString("... (truncated)\n")
			break
		}
		if e.IsDir() {
			b.WriteString(e.Name() + "/\n")
		} else {
			b.WriteString(e.Name() + "\n")
		}
	}
	return b.String(), nil
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return trunc(string(data), 100_000), nil
}

func writeFile(path, content string) (string, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

func editFile(path, target, replacement string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := string(data)
	switch n := strings.Count(text, target); {
	case n == 0:
		return "", fmt.Errorf("target text not found in %s", path)
	case n > 1:
		return "", fmt.Errorf("target text occurs %d times; make it more specific", n)
	}
	text = strings.Replace(text, target, replacement, 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", err
	}
	return "edited " + path, nil
}

func runShell(ctx context.Context, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command)
	} else {
		sh := "bash"
		if _, err := exec.LookPath("bash"); err != nil {
			sh = "sh"
		}
		cmd = exec.CommandContext(ctx, sh, "-c", command)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
			stderr.WriteString(err.Error())
		}
	}
	return fmt.Sprintf("[exit %d]\nstdout:\n%s\nstderr:\n%s",
		code, trunc(stdout.String(), 20_000), trunc(stderr.String(), 10_000)), nil
}

func systemInfo() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	if c, err := cpu.Percent(time.Second, false); err == nil && len(c) > 0 {
		fmt.Fprintf(&b, "CPU: %.1f%%\n", c[0])
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		fmt.Fprintf(&b, "RAM: %.1f%% used (%d of %d MB)\n", vm.UsedPercent, vm.Used>>20, vm.Total>>20)
	}
	diskPath := "/"
	if runtime.GOOS == "windows" {
		diskPath = `C:\`
	}
	if d, err := disk.Usage(diskPath); err == nil {
		fmt.Fprintf(&b, "Disk (%s): %.1f%% used\n", diskPath, d.UsedPercent)
	}
	if bats, err := battery.GetAll(); err == nil && len(bats) > 0 && bats[0].Full > 0 {
		fmt.Fprintf(&b, "Battery: %.0f%% (%v)\n", bats[0].Current/bats[0].Full*100, bats[0].State)
	} else {
		b.WriteString("Battery: none detected\n")
	}
	if ps, err := process.Processes(); err == nil {
		browsers := 0
		for _, p := range ps {
			name, _ := p.Name()
			l := strings.ToLower(name)
			if strings.Contains(l, "chrome") || strings.Contains(l, "firefox") ||
				strings.Contains(l, "safari") || strings.Contains(l, "msedge") {
				browsers++
			}
		}
		fmt.Fprintf(&b, "Processes: %d running (%d browser processes)\n", len(ps), browsers)
	}
	return b.String(), nil
}

func openTarget(target string) (string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	return "opened " + target, nil
}

func clipboardTool(action, text string) (string, error) {
	switch action {
	case "read":
		return clipboard.ReadAll()
	case "write":
		if err := clipboard.WriteAll(text); err != nil {
			return "", err
		}
		return "copied to clipboard", nil
	}
	return "", fmt.Errorf("unknown action %q (use read or write)", action)
}

func trunc(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... (truncated)"
}

// keep json imported for agent.go helpers that share this file's package
var _ = json.Valid
