// Package state holds the in-memory session store for claudewatch.
//
// All access goes through Store, which serializes reads and writes via a
// sync.RWMutex. Handlers must not touch the underlying map directly.
package state

import (
	"errors"
	"sort"
	"sync"
	"time"
)

const (
	StatusIdle        = "idle"
	StatusInputNeeded = "input_needed"
	StatusComplete    = "complete"

	// MetaStatusPending is the meta-filter that matches input_needed | complete.
	MetaStatusPending = "pending"

	ProjectUngrouped = "ungrouped"
)

// SessionState is a snapshot of one tmux session's state. Returned by Store
// methods as a value (not a pointer to internal state) so callers can't mutate
// the store without going through the API.
type SessionState struct {
	Name         string     `json:"name"`
	Project      string     `json:"project"`
	Worktree     string     `json:"worktree"`
	Status       string     `json:"status"`
	LastEvent    time.Time  `json:"last_event"`
	LastMessage  string     `json:"last_message,omitempty"`
	SnoozedUntil *time.Time `json:"snoozed_until,omitempty"`
}

// IsSnoozed reports whether the session is currently suppressing notifications.
func (s SessionState) IsSnoozed(now time.Time) bool {
	return s.SnoozedUntil != nil && s.SnoozedUntil.After(now)
}

// IsPending reports whether the session is awaiting Clayton's attention.
func (s SessionState) IsPending() bool {
	return s.Status == StatusInputNeeded || s.Status == StatusComplete
}

// Filter narrows a List call. Empty fields are wildcards.
type Filter struct {
	Status  string // "idle", "input_needed", "complete", or "pending" meta-status
	Project string
}

// Summary is the aggregate view used by /summary and the tmux status line.
type Summary struct {
	TotalPending     int            `json:"total_pending"`
	ByProject        map[string]int `json:"by_project"`
	OldestAgeSeconds int64          `json:"oldest_age_seconds"`
}

// ErrNotFound is returned when a named session doesn't exist.
var ErrNotFound = errors.New("session not found")

// Store is the in-memory session map. Safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*SessionState
	now      func() time.Time // injected for tests
}

// NewStore returns a new empty Store using time.Now as the clock.
func NewStore() *Store {
	return &Store{
		sessions: make(map[string]*SessionState),
		now:      time.Now,
	}
}

// Upsert sets a session's status from a hook event, creating it if needed.
// Used by POST /notify. Always updates LastEvent. Empty project becomes
// "ungrouped".
func (s *Store) Upsert(name, project, worktree, status, message string) SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.sessions[name]
	if !ok {
		cur = &SessionState{Name: name}
		s.sessions[name] = cur
	}
	if project == "" {
		project = ProjectUngrouped
	}
	cur.Project = project
	if worktree != "" {
		cur.Worktree = worktree
	}
	cur.Status = status
	cur.LastEvent = s.now()
	cur.LastMessage = message
	return *cur
}

// Register adds a session in idle status. Idempotent: if the session already
// exists, project/worktree are filled in only when previously empty, and
// status is left untouched.
func (s *Store) Register(name, project, worktree string) SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if project == "" {
		project = ProjectUngrouped
	}
	cur, ok := s.sessions[name]
	if !ok {
		cur = &SessionState{
			Name:      name,
			Project:   project,
			Worktree:  worktree,
			Status:    StatusIdle,
			LastEvent: s.now(),
		}
		s.sessions[name] = cur
		return *cur
	}
	if cur.Project == "" || cur.Project == ProjectUngrouped {
		cur.Project = project
	}
	if cur.Worktree == "" {
		cur.Worktree = worktree
	}
	return *cur
}

// Get returns a copy of the named session.
func (s *Store) Get(name string) (SessionState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cur, ok := s.sessions[name]
	if !ok {
		return SessionState{}, false
	}
	return *cur, true
}

// List returns sessions matching the filter, sorted by LastEvent descending
// (newest first). The "pending" status is a meta-filter for input_needed |
// complete.
func (s *Store) List(filter Filter) []SessionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SessionState, 0, len(s.sessions))
	for _, sess := range s.sessions {
		if !matchFilter(*sess, filter) {
			continue
		}
		out = append(out, *sess)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastEvent.After(out[j].LastEvent)
	})
	return out
}

func matchFilter(s SessionState, f Filter) bool {
	if f.Project != "" && s.Project != f.Project {
		return false
	}
	switch f.Status {
	case "":
		return true
	case MetaStatusPending:
		return s.IsPending()
	default:
		return s.Status == f.Status
	}
}

// PendingOldest returns the pending session with the oldest LastEvent that is
// not currently snoozed. Used by GET /pending/oldest.
func (s *Store) PendingOldest() (SessionState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.now()
	var oldest *SessionState
	for _, sess := range s.sessions {
		if !sess.IsPending() {
			continue
		}
		if sess.IsSnoozed(now) {
			continue
		}
		if oldest == nil || sess.LastEvent.Before(oldest.LastEvent) {
			oldest = sess
		}
	}
	if oldest == nil {
		return SessionState{}, false
	}
	return *oldest, true
}

// Clear sets a session back to idle and clears any snooze. Idempotent.
func (s *Store) Clear(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.sessions[name]
	if !ok {
		return
	}
	cur.Status = StatusIdle
	cur.SnoozedUntil = nil
	cur.LastEvent = s.now()
	cur.LastMessage = ""
}

// Snooze suppresses notifications for the session for the given duration.
// Returns ErrNotFound if the session doesn't exist.
func (s *Store) Snooze(name string, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.sessions[name]
	if !ok {
		return ErrNotFound
	}
	until := s.now().Add(d)
	cur.SnoozedUntil = &until
	return nil
}

// Delete removes a session from the store. Returns true if it existed.
func (s *Store) Delete(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[name]; !ok {
		return false
	}
	delete(s.sessions, name)
	return true
}

// Summary returns aggregate counts. Snoozed sessions are excluded from
// total_pending and oldest_age_seconds (they shouldn't surface in the status
// line until the snooze expires).
func (s *Store) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.now()
	out := Summary{ByProject: map[string]int{}}
	var oldest time.Time
	for _, sess := range s.sessions {
		if !sess.IsPending() || sess.IsSnoozed(now) {
			continue
		}
		out.TotalPending++
		out.ByProject[sess.Project]++
		if oldest.IsZero() || sess.LastEvent.Before(oldest) {
			oldest = sess.LastEvent
		}
	}
	if !oldest.IsZero() {
		out.OldestAgeSeconds = int64(now.Sub(oldest).Seconds())
	}
	return out
}
