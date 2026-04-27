package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/cfstout/claudewatch/internal/state"
)

func TestRenderAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{2 * time.Minute, "2m"},
		{3 * time.Hour, "3h"},
		{50 * time.Hour, "2d"},
	}
	for _, c := range cases {
		if got := renderAge(c.d); got != c.want {
			t.Errorf("renderAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestFormatSummaryTmuxEmpty(t *testing.T) {
	if got := FormatSummaryTmux(state.Summary{}); got != "" {
		t.Errorf("empty summary should be blank, got %q", got)
	}
}

func TestFormatSummaryTmuxPending(t *testing.T) {
	got := FormatSummaryTmux(state.Summary{TotalPending: 3, OldestAgeSeconds: 600})
	if !strings.Contains(got, "◆3") {
		t.Errorf("missing ◆3 in %q", got)
	}
	if !strings.Contains(got, "10m") {
		t.Errorf("missing 10m in %q", got)
	}
}

func TestPrintSessionsEmptyDoesNotPanic(t *testing.T) {
	var buf bytes.Buffer
	PrintSessions(&buf, nil)
	if !strings.Contains(buf.String(), "no sessions") {
		t.Errorf("expected 'no sessions' marker, got %q", buf.String())
	}
}

func TestPrintSessionsRendersAllRows(t *testing.T) {
	now := time.Now()
	sessions := []state.SessionState{
		{Name: "alpha", Project: "demo", Status: state.StatusInputNeeded, LastEvent: now.Add(-30 * time.Second), LastMessage: "blocked"},
		{Name: "beta", Project: "infra", Status: state.StatusComplete, LastEvent: now.Add(-90 * time.Second)},
	}
	var buf bytes.Buffer
	PrintSessions(&buf, sessions)
	out := buf.String()
	for _, want := range []string{"alpha", "beta", "demo", "infra", "blocked"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
