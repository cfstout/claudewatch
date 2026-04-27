package cli

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/cfstout/claudewatch/internal/state"
	"golang.org/x/term"
)

const (
	colReset  = "\x1b[0m"
	colRed    = "\x1b[31m"
	colGreen  = "\x1b[32m"
	colDim    = "\x1b[2m"
	colYellow = "\x1b[33m"
)

// useColor decides whether ANSI escapes go to w. We treat anything that's a
// TTY as eligible. The watch -n 1 cw dashboard is a TTY; piping into scripts
// is not.
func useColor(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// PrintSessions renders sessions as an aligned table to w.
func PrintSessions(w io.Writer, sessions []state.SessionState) {
	color := useColor(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tPROJECT\tSESSION\tAGE\tMESSAGE")
	now := time.Now()
	if len(sessions) == 0 {
		fmt.Fprintln(tw, "(no sessions)\t\t\t\t")
		_ = tw.Flush()
		return
	}
	for _, s := range sessions {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			renderStatus(s, color, now),
			s.Project,
			s.Name,
			renderAge(now.Sub(s.LastEvent)),
			truncateMsg(s.LastMessage, 60),
		)
	}
	_ = tw.Flush()
}

func renderStatus(s state.SessionState, color bool, now time.Time) string {
	glyph := "·"
	col := ""
	switch s.Status {
	case state.StatusInputNeeded:
		glyph = "◆ input"
		col = colRed
	case state.StatusComplete:
		glyph = "✓ done"
		col = colGreen
	case state.StatusIdle:
		glyph = "· idle"
		col = colDim
	}
	if s.IsSnoozed(now) {
		glyph += " (snoozed)"
		col = colYellow
	}
	if !color || col == "" {
		return glyph
	}
	return col + glyph + colReset
}

func renderAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func truncateMsg(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// FormatSummaryTmux renders the summary as a one-line tmux status fragment.
// Returns empty string when nothing is pending — keeps the status line clean.
func FormatSummaryTmux(sum state.Summary) string {
	if sum.TotalPending == 0 {
		return ""
	}
	age := renderAge(time.Duration(sum.OldestAgeSeconds) * time.Second)
	return fmt.Sprintf("#[fg=red]◆%d #[fg=default]oldest %s ", sum.TotalPending, age)
}
