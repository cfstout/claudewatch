package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/cfstout/claudewatch/internal/state"
	"github.com/go-chi/chi/v5"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleNotify processes a Claude Code hook event.
// Form: session (required), type (required: "notification"|"stop"),
// project, worktree, message (optional).
func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	name := r.PostFormValue("session")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing session")
		return
	}
	hookType := r.PostFormValue("type")
	var status string
	switch hookType {
	case "notification":
		status = state.StatusInputNeeded
	case "stop":
		status = state.StatusComplete
	default:
		writeError(w, http.StatusBadRequest, "type must be 'notification' or 'stop'")
		return
	}
	sess := s.Store.Upsert(
		name,
		r.PostFormValue("project"),
		r.PostFormValue("worktree"),
		status,
		r.PostFormValue("message"),
	)
	// Snoozed sessions still record the event but don't trigger a popup.
	if !sess.IsSnoozed(time.Now()) {
		s.Notifier.Fire(sess)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleRegisterSession registers an idle session (called from `dev`).
// Form: session (required), project, worktree (optional).
func (s *Server) handleRegisterSession(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	name := r.PostFormValue("session")
	if name == "" {
		writeError(w, http.StatusBadRequest, "missing session")
		return
	}
	s.Store.Register(name, r.PostFormValue("project"), r.PostFormValue("worktree"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out := s.Store.List(state.Filter{
		Status:  q.Get("status"),
		Project: q.Get("project"),
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	sess, ok := s.Store.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s.Store.Delete(name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleClearSession(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s.Store.Clear(name)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSnoozeSession parses {"minutes": N} from the JSON body. Falls back
// to DefaultSnooze when minutes is omitted, zero, or negative.
func (s *Server) handleSnoozeSession(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	d := s.DefaultSnooze
	if r.ContentLength > 0 {
		var body struct {
			Minutes int `json:"minutes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if body.Minutes > 0 {
			d = time.Duration(body.Minutes) * time.Minute
		}
	}
	if err := s.Store.Snooze(name, d); err != nil {
		if errors.Is(err, state.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"snoozed_for_s":  strconv.FormatInt(int64(d.Seconds()), 10),
	})
}

func (s *Server) handlePendingOldest(w http.ResponseWriter, _ *http.Request) {
	sess, ok := s.Store.PendingOldest()
	if !ok {
		writeError(w, http.StatusNotFound, "no pending sessions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": sess.Name})
}

func (s *Server) handleSummary(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Store.Summary())
}
