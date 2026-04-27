package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cfstout/claudewatch/internal/state"
)

// recordingNotifier captures Fire calls for assertion.
type recordingNotifier struct {
	mu    sync.Mutex
	fired []state.SessionState
}

func (r *recordingNotifier) Fire(s state.SessionState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fired = append(r.fired, s)
}

func (r *recordingNotifier) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.fired))
	for i, s := range r.fired {
		out[i] = s.Name
	}
	return out
}

func newTestServer(t *testing.T) (*httptest.Server, *state.Store, *recordingNotifier) {
	t.Helper()
	store := state.NewStore()
	notifier := &recordingNotifier{}
	srv := New(store)
	srv.Notifier = notifier
	srv.DefaultSnooze = 10 * time.Minute
	hs := httptest.NewServer(srv.Router())
	t.Cleanup(hs.Close)
	return hs, store, notifier
}

func postForm(t *testing.T, base, path string, form url.Values) *http.Response {
	t.Helper()
	resp, err := http.PostForm(base+path, form)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func get(t *testing.T, base, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, into any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func mustOK(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	resp.Body.Close()
}

func TestHealthz(t *testing.T) {
	hs, _, _ := newTestServer(t)
	resp := get(t, hs.URL, "/healthz")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body["ok"] {
		t.Fatalf("body = %v", body)
	}
}

func TestNotifyCreatesSessionAndFiresNotifier(t *testing.T) {
	hs, store, notifier := newTestServer(t)
	resp := postForm(t, hs.URL, "/notify", url.Values{
		"session":  {"alpha"},
		"type":     {"notification"},
		"project":  {"demo"},
		"worktree": {"/tmp/a"},
		"message":  {"blocked on review"},
	})
	mustOK(t, resp)

	got, ok := store.Get("alpha")
	if !ok || got.Status != state.StatusInputNeeded {
		t.Fatalf("session not stored as input_needed: %+v", got)
	}
	if got.LastMessage != "blocked on review" {
		t.Fatalf("message = %q", got.LastMessage)
	}
	if names := notifier.Names(); len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("notifier fires = %v, want [alpha]", names)
	}
}

func TestNotifyMissingSessionRejected(t *testing.T) {
	hs, _, _ := newTestServer(t)
	resp := postForm(t, hs.URL, "/notify", url.Values{"type": {"notification"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestNotifyInvalidTypeRejected(t *testing.T) {
	hs, _, _ := newTestServer(t)
	resp := postForm(t, hs.URL, "/notify", url.Values{
		"session": {"alpha"},
		"type":    {"bogus"},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRegisterSessionIdempotent(t *testing.T) {
	hs, store, _ := newTestServer(t)

	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"alpha"},
		"type":    {"notification"},
	}))
	mustOK(t, postForm(t, hs.URL, "/sessions", url.Values{
		"session": {"alpha"},
		"project": {"demo"},
	}))

	got, _ := store.Get("alpha")
	if got.Status != state.StatusInputNeeded {
		t.Fatalf("Register reset status: %q", got.Status)
	}
	if got.Project != "demo" {
		t.Fatalf("Register did not fill in project: %q", got.Project)
	}
}

func TestListWithFilters(t *testing.T) {
	hs, _, _ := newTestServer(t)
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"alpha"}, "type": {"notification"}, "project": {"demo"},
	}))
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"beta"}, "type": {"stop"}, "project": {"infra"},
	}))
	mustOK(t, postForm(t, hs.URL, "/sessions", url.Values{
		"session": {"gamma"}, "project": {"demo"},
	}))

	resp := get(t, hs.URL, "/sessions?status=pending")
	var sessions []state.SessionState
	decode(t, resp, &sessions)
	if len(sessions) != 2 {
		t.Fatalf("pending list = %d, want 2", len(sessions))
	}

	resp = get(t, hs.URL, "/sessions?project=demo")
	decode(t, resp, &sessions)
	if len(sessions) != 2 {
		t.Fatalf("project=demo list = %d, want 2", len(sessions))
	}
}

func TestPendingOldestRoundTrip(t *testing.T) {
	hs, _, _ := newTestServer(t)
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"old"}, "type": {"notification"},
	}))
	time.Sleep(10 * time.Millisecond)
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"new"}, "type": {"notification"},
	}))

	resp := get(t, hs.URL, "/pending/oldest")
	var got map[string]string
	decode(t, resp, &got)
	if got["name"] != "old" {
		t.Fatalf("pending oldest = %q, want old", got["name"])
	}
}

func TestPendingOldestEmpty(t *testing.T) {
	hs, _, _ := newTestServer(t)
	resp := get(t, hs.URL, "/pending/oldest")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("empty pending = %d, want 404", resp.StatusCode)
	}
}

func TestSnoozeSuppressesPending(t *testing.T) {
	hs, _, _ := newTestServer(t)
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"alpha"}, "type": {"notification"},
	}))

	body := strings.NewReader(`{"minutes": 60}`)
	resp, err := http.Post(hs.URL+"/sessions/alpha/snooze", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	mustOK(t, resp)

	resp = get(t, hs.URL, "/pending/oldest")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("snoozed session still in /pending/oldest: %d", resp.StatusCode)
	}
}

func TestSnoozeUnknownSession404(t *testing.T) {
	hs, _, _ := newTestServer(t)
	resp, err := http.Post(hs.URL+"/sessions/nope/snooze", "application/json", strings.NewReader(`{"minutes":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestSnoozeNoBodyUsesDefault(t *testing.T) {
	hs, store, _ := newTestServer(t)
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"alpha"}, "type": {"notification"},
	}))
	resp, err := http.Post(hs.URL+"/sessions/alpha/snooze", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	mustOK(t, resp)
	got, _ := store.Get("alpha")
	if got.SnoozedUntil == nil {
		t.Fatal("default snooze did not set SnoozedUntil")
	}
}

func TestClearResetsToIdle(t *testing.T) {
	hs, store, _ := newTestServer(t)
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"alpha"}, "type": {"notification"},
	}))
	resp, err := http.Post(hs.URL+"/sessions/alpha/clear", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustOK(t, resp)
	got, _ := store.Get("alpha")
	if got.Status != state.StatusIdle {
		t.Fatalf("status = %q, want idle", got.Status)
	}
}

func TestDeleteRemovesSession(t *testing.T) {
	hs, store, _ := newTestServer(t)
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"alpha"}, "type": {"notification"},
	}))
	req, _ := http.NewRequest(http.MethodDelete, hs.URL+"/sessions/alpha", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	mustOK(t, resp)
	if _, ok := store.Get("alpha"); ok {
		t.Fatal("session still present after DELETE")
	}
}

func TestSummary(t *testing.T) {
	hs, _, _ := newTestServer(t)
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"a"}, "type": {"notification"}, "project": {"demo"},
	}))
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"b"}, "type": {"stop"}, "project": {"infra"},
	}))

	resp := get(t, hs.URL, "/summary")
	var got state.Summary
	decode(t, resp, &got)
	if got.TotalPending != 2 {
		t.Fatalf("total_pending = %d, want 2", got.TotalPending)
	}
	if got.ByProject["demo"] != 1 || got.ByProject["infra"] != 1 {
		t.Fatalf("by_project = %+v", got.ByProject)
	}
}

func TestNotifySnoozedSkipsNotifier(t *testing.T) {
	hs, _, notifier := newTestServer(t)
	// Initial notify creates the session and fires once.
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"alpha"}, "type": {"notification"},
	}))
	// Snooze for an hour.
	resp, err := http.Post(hs.URL+"/sessions/alpha/snooze", "application/json", strings.NewReader(`{"minutes":60}`))
	if err != nil {
		t.Fatal(err)
	}
	mustOK(t, resp)
	// Second notify event should NOT fire the notifier.
	mustOK(t, postForm(t, hs.URL, "/notify", url.Values{
		"session": {"alpha"}, "type": {"notification"},
	}))
	if got := notifier.Names(); len(got) != 1 {
		t.Fatalf("snoozed notify fired notifier: %v", got)
	}
}

func TestEscapeAppleScript(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`hello`, `hello`},
		{`he said "hi"`, `he said \"hi\"`},
		{`back\slash`, `back\\slash`},
		{`"\"`, `\"\\\"`},
	}
	for _, c := range cases {
		if got := escapeAppleScript(c.in); got != c.want {
			t.Errorf("escapeAppleScript(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatNotification(t *testing.T) {
	cases := []struct {
		s     state.SessionState
		title string
		body  string
	}{
		{
			state.SessionState{Name: "alpha", Project: "demo", Status: state.StatusInputNeeded},
			"Claude · demo/alpha",
			"needs your input",
		},
		{
			state.SessionState{Name: "alpha", Project: state.ProjectUngrouped, Status: state.StatusComplete},
			"Claude · alpha",
			"task complete",
		},
		{
			state.SessionState{Name: "alpha", Project: "", Status: state.StatusInputNeeded, LastMessage: "hi"},
			"Claude · alpha",
			"needs your input — hi",
		},
	}
	for _, c := range cases {
		title, body := formatNotification(c.s)
		if title != c.title || body != c.body {
			t.Errorf("formatNotification(%+v) = (%q, %q), want (%q, %q)",
				c.s, title, body, c.title, c.body)
		}
	}
}

func TestOSNotifierDebounce(t *testing.T) {
	n := NewOSNotifier(time.Hour) // long debounce, but Fire should record only the first
	// Inspect internal state: after one Fire, last[name] is set; second Fire
	// within window should not update last (i.e., still equals first call's time).
	s := state.SessionState{Name: "alpha", Status: state.StatusInputNeeded}
	n.Fire(s)
	first := n.last["alpha"]
	time.Sleep(2 * time.Millisecond)
	n.Fire(s)
	second := n.last["alpha"]
	if !first.Equal(second) {
		t.Fatalf("debounce did not suppress second Fire: first=%v second=%v", first, second)
	}
}
