# claudewatch

Local Go daemon for tracking Claude Code session state across many tmux
worktrees. Single user, localhost only, in-memory state, launchd user agent
on macOS.

The full design lives in the PRD (`docs/PRD.md` if/when checked in). This
README covers usage and the one place the implementation departs from it.

## Install

```sh
make install
curl -fsS localhost:7777/healthz   # must return {"ok":true} before wiring hooks
```

Installs to `~/.local/bin/claudewatch` plus a `cw` symlink. The launchd plist
goes to `~/Library/LaunchAgents/com.cfstout.claudewatch.plist`. Logs land in
`~/Library/Logs/claudewatch/{stdout,stderr}.log`.

## Uninstall

```sh
make uninstall
```

## Usage

| Command                          | Purpose                                       |
|----------------------------------|-----------------------------------------------|
| `cw`                             | Default: list pending sessions                |
| `cw next`                        | Print oldest pending session name (exit 1 if none) |
| `cw clear <name>`                | Mark a session idle                           |
| `cw snooze <name> [--minutes N]` | Suppress a session                            |
| `cw summary [--format tmux\|json]` | Aggregate counts                            |
| `claudewatch daemon [--port N]`  | Run the daemon in the foreground              |

## Config

Optional `~/.config/claudewatch/config.toml`:

```toml
port = 7777
notification_command = "auto"           # "auto" picks terminal-notifier, falls back to osascript
notifications_enabled = true            # set false to silence claudewatch's banners (e.g., if you use claude-notifications-go)
debounce_seconds = 10
default_snooze_minutes = 10
auto_archive_complete_minutes = 0       # 0 = never (reserved for v1.x; not yet enforced)
```

If you have `claude-notifications-go` (or another notification plugin) registered as a Claude Code hook, claudewatch's banners will fire on top of it — set `notifications_enabled = false` to keep the queue / status-line / `prefix+N` workflow without duplicate banners.

All fields optional; missing or malformed file falls back to defaults.

## API path style — note vs PRD

The PRD specified two URL forms: `POST /clear/:session` and `POST /sessions`.
This implementation consolidates per-session operations under a single nested
form:

| Operation | URL                                |
|-----------|------------------------------------|
| Notify    | `POST /notify` (form body)         |
| Register  | `POST /sessions` (form body)       |
| List      | `GET /sessions[?status=…&project=…]` |
| Get       | `GET /sessions/{name}`             |
| Delete    | `DELETE /sessions/{name}`          |
| Clear     | `POST /sessions/{name}/clear`      |
| Snooze    | `POST /sessions/{name}/snooze` (JSON `{"minutes":N}`) |
| Pending   | `GET /pending/oldest`              |
| Summary   | `GET /summary`                     |
| Health    | `GET /healthz`                     |

## Hook integration (Claude Code, tmux, dev)

See [`setup.md`](./setup.md) for the snippets that go into
`~/.claude/settings.json`, `~/.tmux.conf`, and the `dev` shell function.
