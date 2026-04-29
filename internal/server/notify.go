package server

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cfstout/claudewatch/internal/state"
)

// OSNotifier dispatches macOS notifications via terminal-notifier (preferred)
// or osascript (fallback). Per-session debouncing prevents notification floods
// when hooks fire rapidly.
type OSNotifier struct {
	debounce time.Duration

	mu   sync.Mutex
	last map[string]time.Time
}

// NewOSNotifier returns a notifier with the given debounce window. A debounce
// of 0 disables it.
func NewOSNotifier(debounce time.Duration) *OSNotifier {
	return &OSNotifier{
		debounce: debounce,
		last:     make(map[string]time.Time),
	}
}

// Fire dispatches a notification for the session if outside the debounce
// window. Non-blocking — the actual exec runs in a goroutine.
func (n *OSNotifier) Fire(s state.SessionState) {
	now := time.Now()
	n.mu.Lock()
	if last, ok := n.last[s.Name]; ok && now.Sub(last) < n.debounce {
		n.mu.Unlock()
		return
	}
	n.last[s.Name] = now
	n.mu.Unlock()

	title, body := formatNotification(s)
	go dispatch(title, body)
}

// formatNotification renders the banner title + body for a session event.
// Emoji prefix encodes the status at a glance — borrowing the convention
// from claude-notifications-go (❓ for input needed, ✅ for complete) so
// notifications skim well in Notification Center.
func formatNotification(s state.SessionState) (title, body string) {
	var prefix string
	switch s.Status {
	case state.StatusInputNeeded:
		prefix = "❓"
		body = "needs your input"
	case state.StatusComplete:
		prefix = "✅"
		body = "task complete"
	default:
		prefix = "·"
		body = s.Status
	}
	if s.Project == state.ProjectUngrouped || s.Project == "" {
		title = fmt.Sprintf("%s %s", prefix, s.Name)
	} else {
		title = fmt.Sprintf("%s %s · %s", prefix, s.Project, s.Name)
	}
	if s.LastMessage != "" {
		body = body + " — " + truncate(s.LastMessage, 140)
	}
	return title, body
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// terminalNotifierCandidates is the search path for terminal-notifier,
// in order. The first existing executable wins. exec.LookPath alone isn't
// reliable because launchd may start the daemon with a minimal PATH that
// excludes Homebrew prefixes.
var terminalNotifierCandidates = []string{
	"/opt/homebrew/bin/terminal-notifier",
	"/usr/local/bin/terminal-notifier",
}

func findTerminalNotifier() string {
	if p, err := exec.LookPath("terminal-notifier"); err == nil {
		return p
	}
	for _, p := range terminalNotifierCandidates {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	return ""
}

// dispatch shells out to terminal-notifier or osascript. Logs failures via
// slog but never returns an error — notifications are best-effort.
func dispatch(title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if path := findTerminalNotifier(); path != "" {
		err := exec.CommandContext(ctx, path,
			"-title", title,
			"-message", body,
			"-group", "claudewatch",
		).Run()
		if err == nil {
			return
		}
		slog.Warn("terminal-notifier failed; falling back to osascript", "err", err)
	}

	script := fmt.Sprintf(
		`display notification "%s" with title "%s"`,
		escapeAppleScript(body),
		escapeAppleScript(title),
	)
	if err := exec.CommandContext(ctx, "osascript", "-e", script).Run(); err != nil {
		slog.Warn("osascript failed", "err", err)
	}
}

// escapeAppleScript escapes backslashes and double quotes for use inside a
// double-quoted AppleScript string literal.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
