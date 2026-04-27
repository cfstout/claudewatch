package state

import (
	"testing"
	"time"
)

// fixedClock returns a Store whose now() returns a controllable time.
func fixedClock(t time.Time) (*Store, *time.Time) {
	current := t
	s := NewStore()
	s.now = func() time.Time { return current }
	return s, &current
}

func TestUpsertCreatesAndUpdates(t *testing.T) {
	s, now := fixedClock(time.Unix(1000, 0))

	got := s.Upsert("alpha", "demo", "/tmp/a", StatusInputNeeded, "needs you")
	if got.Status != StatusInputNeeded {
		t.Fatalf("status = %q, want %q", got.Status, StatusInputNeeded)
	}
	if got.Project != "demo" || got.Worktree != "/tmp/a" {
		t.Fatalf("project/worktree wrong: %+v", got)
	}
	if !got.LastEvent.Equal(*now) {
		t.Fatalf("last_event = %v, want %v", got.LastEvent, *now)
	}

	*now = now.Add(5 * time.Second)
	got = s.Upsert("alpha", "demo", "/tmp/a", StatusComplete, "")
	if got.Status != StatusComplete {
		t.Fatalf("status not updated: %q", got.Status)
	}
	if !got.LastEvent.Equal(*now) {
		t.Fatalf("last_event not advanced")
	}
}

func TestUpsertEmptyProjectBecomesUngrouped(t *testing.T) {
	s, _ := fixedClock(time.Unix(0, 0))
	got := s.Upsert("alpha", "", "", StatusInputNeeded, "")
	if got.Project != ProjectUngrouped {
		t.Fatalf("project = %q, want %q", got.Project, ProjectUngrouped)
	}
}

func TestRegisterIdempotentDoesNotResetStatus(t *testing.T) {
	s, _ := fixedClock(time.Unix(1000, 0))
	s.Upsert("alpha", "demo", "/tmp/a", StatusInputNeeded, "blocked")

	got := s.Register("alpha", "demo", "/tmp/a")
	if got.Status != StatusInputNeeded {
		t.Fatalf("Register reset status to %q (should preserve input_needed)", got.Status)
	}
}

func TestRegisterFillsMissingProjectWorktree(t *testing.T) {
	s, _ := fixedClock(time.Unix(1000, 0))
	// Notify event arrives without project/worktree.
	s.Upsert("alpha", "", "", StatusInputNeeded, "")
	// Then dev registers it with full info.
	got := s.Register("alpha", "demo", "/tmp/a")
	if got.Project != "demo" {
		t.Fatalf("project not filled in: %q", got.Project)
	}
	if got.Worktree != "/tmp/a" {
		t.Fatalf("worktree not filled in: %q", got.Worktree)
	}
}

func TestNotifySnoozeClearNotifyCycle(t *testing.T) {
	s, now := fixedClock(time.Unix(1000, 0))

	s.Upsert("alpha", "demo", "/tmp/a", StatusInputNeeded, "blocked")

	// Snooze for 1 minute.
	if err := s.Snooze("alpha", time.Minute); err != nil {
		t.Fatalf("Snooze: %v", err)
	}
	if got, _ := s.PendingOldest(); got.Name != "" {
		t.Fatalf("PendingOldest returned snoozed session: %+v", got)
	}

	// Clock advances past snooze.
	*now = now.Add(2 * time.Minute)
	got, ok := s.PendingOldest()
	if !ok || got.Name != "alpha" {
		t.Fatalf("PendingOldest after snooze expiry: ok=%v got=%+v", ok, got)
	}

	// Clear → no longer pending.
	s.Clear("alpha")
	if got, ok := s.PendingOldest(); ok {
		t.Fatalf("PendingOldest after Clear: %+v", got)
	}

	// New event → pending again, snooze gone.
	s.Upsert("alpha", "demo", "/tmp/a", StatusInputNeeded, "still blocked")
	got, ok = s.PendingOldest()
	if !ok || got.Name != "alpha" {
		t.Fatalf("PendingOldest after re-notify: ok=%v got=%+v", ok, got)
	}
	if got.SnoozedUntil != nil {
		t.Fatalf("SnoozedUntil should be cleared after Clear, got %v", got.SnoozedUntil)
	}
}

func TestSnoozeUnknownSession(t *testing.T) {
	s, _ := fixedClock(time.Unix(0, 0))
	if err := s.Snooze("nope", time.Minute); err != ErrNotFound {
		t.Fatalf("Snooze unknown: err = %v, want ErrNotFound", err)
	}
}

func TestPendingOldestPicksOldest(t *testing.T) {
	s, now := fixedClock(time.Unix(1000, 0))
	s.Upsert("alpha", "demo", "", StatusInputNeeded, "")
	*now = now.Add(time.Second)
	s.Upsert("beta", "demo", "", StatusComplete, "")
	*now = now.Add(time.Second)
	s.Upsert("gamma", "demo", "", StatusInputNeeded, "")

	got, ok := s.PendingOldest()
	if !ok || got.Name != "alpha" {
		t.Fatalf("PendingOldest = %+v, want alpha", got)
	}
}

func TestPendingOldestSkipsIdle(t *testing.T) {
	s, _ := fixedClock(time.Unix(1000, 0))
	s.Register("alpha", "demo", "")
	s.Register("beta", "demo", "")
	if got, ok := s.PendingOldest(); ok {
		t.Fatalf("PendingOldest with only idle sessions: %+v", got)
	}
}

func TestListFilterByStatusPending(t *testing.T) {
	s, _ := fixedClock(time.Unix(1000, 0))
	s.Upsert("a", "demo", "", StatusInputNeeded, "")
	s.Upsert("b", "demo", "", StatusComplete, "")
	s.Register("c", "demo", "")

	got := s.List(Filter{Status: MetaStatusPending})
	if len(got) != 2 {
		t.Fatalf("pending list len = %d, want 2: %+v", len(got), got)
	}
	for _, sess := range got {
		if sess.Status != StatusInputNeeded && sess.Status != StatusComplete {
			t.Fatalf("non-pending in pending filter: %+v", sess)
		}
	}
}

func TestListFilterByProject(t *testing.T) {
	s, _ := fixedClock(time.Unix(1000, 0))
	s.Upsert("a", "demo", "", StatusInputNeeded, "")
	s.Upsert("b", "infra", "", StatusInputNeeded, "")

	got := s.List(Filter{Project: "infra"})
	if len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("project filter: %+v", got)
	}
}

func TestListSortedByLastEventDesc(t *testing.T) {
	s, now := fixedClock(time.Unix(1000, 0))
	s.Upsert("first", "demo", "", StatusInputNeeded, "")
	*now = now.Add(time.Second)
	s.Upsert("second", "demo", "", StatusInputNeeded, "")

	got := s.List(Filter{})
	if len(got) != 2 || got[0].Name != "second" || got[1].Name != "first" {
		t.Fatalf("not sorted desc by LastEvent: %+v", got)
	}
}

func TestSummaryExcludesSnoozed(t *testing.T) {
	s, _ := fixedClock(time.Unix(1000, 0))
	s.Upsert("a", "demo", "", StatusInputNeeded, "")
	s.Upsert("b", "infra", "", StatusComplete, "")
	if err := s.Snooze("a", time.Hour); err != nil {
		t.Fatal(err)
	}

	sum := s.Summary()
	if sum.TotalPending != 1 {
		t.Fatalf("total_pending = %d, want 1 (snoozed should be excluded)", sum.TotalPending)
	}
	if sum.ByProject["infra"] != 1 || sum.ByProject["demo"] != 0 {
		t.Fatalf("by_project = %+v", sum.ByProject)
	}
}

func TestSummaryOldestAge(t *testing.T) {
	s, now := fixedClock(time.Unix(1000, 0))
	s.Upsert("a", "demo", "", StatusInputNeeded, "")
	*now = now.Add(30 * time.Second)
	s.Upsert("b", "demo", "", StatusInputNeeded, "")
	*now = now.Add(30 * time.Second) // 60s after a's event
	sum := s.Summary()
	if sum.OldestAgeSeconds != 60 {
		t.Fatalf("oldest_age_seconds = %d, want 60", sum.OldestAgeSeconds)
	}
}

func TestDeleteRemoves(t *testing.T) {
	s, _ := fixedClock(time.Unix(1000, 0))
	s.Upsert("a", "demo", "", StatusInputNeeded, "")
	if !s.Delete("a") {
		t.Fatal("Delete returned false for existing session")
	}
	if _, ok := s.Get("a"); ok {
		t.Fatal("session still present after Delete")
	}
	if s.Delete("a") {
		t.Fatal("Delete returned true for already-removed session")
	}
}

func TestConcurrentAccessNoRace(t *testing.T) {
	// Sanity check: hammer the store from multiple goroutines and rely on
	// `go test -race` (run via `make test`) to catch any data races.
	s := NewStore()
	done := make(chan struct{})
	for i := range 8 {
		go func(id int) {
			for j := range 200 {
				name := "s" + string(rune('a'+id))
				s.Upsert(name, "demo", "", StatusInputNeeded, "")
				s.List(Filter{Status: MetaStatusPending})
				s.Summary()
				_, _ = s.PendingOldest()
				if j%4 == 0 {
					s.Clear(name)
				}
			}
			done <- struct{}{}
		}(i)
	}
	for range 8 {
		<-done
	}
}
