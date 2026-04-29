# claudewatch — setup

End-to-end install for a fresh macOS machine. Steps 4–6 are the integration
points; without them the daemon runs but nothing fires events into it.

## Prerequisites

```sh
brew install go terminal-notifier
```

Go 1.22+ is required (stdlib HTTP routing). `terminal-notifier` is preferred
over `osascript` for notification attribution — without it, banners come from
"Script Editor" and click-through opens Xcode.

## 1. Build and install the daemon

```sh
git clone <this-repo> ~/work/claudewatch
cd ~/work/claudewatch
make install
```

This produces `~/.local/bin/claudewatch` (plus a `cw` symlink), generates the
launchd plist at `~/Library/LaunchAgents/com.cfstout.claudewatch.plist`, and
loads it. The plist sets `KeepAlive=true` and a `PATH` that includes
`/opt/homebrew/bin` so `terminal-notifier` resolves correctly.

## 2. Verify

```sh
curl -fsS localhost:7777/healthz   # → {"ok":true}
launchctl list | grep claudewatch  # → PID 0 com.cfstout.claudewatch
```

If healthz fails, check `~/Library/Logs/claudewatch/stderr.log` and re-run
`launchctl kickstart -k gui/$UID/com.cfstout.claudewatch`.

## 3. Trigger a permission prompt

The first time the daemon shells out to `terminal-notifier`, macOS will ask
to allow notifications. Accept it.

```sh
curl -s -X POST localhost:7777/notify \
  --data-urlencode "session=permission-prompt" \
  --data-urlencode "type=notification" \
  --data-urlencode "project=demo"
```

Then click "Allow" on the macOS prompt. Clean up:

```sh
cw clear permission-prompt && cw    # confirm gone from list
```

## 4. Wire up Claude Code hooks

Edit `~/.claude/settings.json` and replace the `Notification` and `Stop`
hook commands. The `[ -n "$TMUX" ]` guard is required — Claude can run
outside tmux and the hook would otherwise POST `session=` (empty).

```json
"Notification": [
  {
    "hooks": [
      {
        "type": "command",
        "command": "[ -n \"$TMUX\" ] && session=$(tmux display-message -p '#S' 2>/dev/null) && [ -n \"$session\" ] && curl -s --max-time 2 -X POST localhost:7777/notify --data-urlencode \"session=$session\" --data-urlencode \"type=notification\" --data-urlencode \"project=$(tmux show-options -v @project 2>/dev/null)\" --data-urlencode \"worktree=$PWD\" --data-urlencode \"message=needs input\" --data-urlencode \"attended=$(tmux list-clients -t \\\"$session\\\" 2>/dev/null | grep -c .)\" >/dev/null 2>&1 || true; printf '\\a'"
      }
    ]
  }
],
"Stop": [
  {
    "hooks": [
      {
        "type": "command",
        "command": "[ -n \"$TMUX\" ] && session=$(tmux display-message -p '#S' 2>/dev/null) && [ -n \"$session\" ] && curl -s --max-time 2 -X POST localhost:7777/notify --data-urlencode \"session=$session\" --data-urlencode \"type=stop\" --data-urlencode \"project=$(tmux show-options -v @project 2>/dev/null)\" --data-urlencode \"worktree=$PWD\" --data-urlencode \"message=task complete\" --data-urlencode \"attended=$(tmux list-clients -t \\\"$session\\\" 2>/dev/null | grep -c .)\" >/dev/null 2>&1 || true; printf '\\a'"
      }
    ]
  }
]
```

Notes on the moving parts in that command:

- `[ -n "$TMUX" ]` guard — Claude can run outside tmux; without this the hook
  would POST `session=` (empty) and pollute daemon state.
- `tmux show-options -v @project` (no `-g`) reads the **per-session** value
  set by the `dev` shell function. Reading globally would cross-attribute
  every notification to whichever session was created last.
- `attended=$(tmux list-clients -t "$session" | grep -c .)` tells the daemon
  whether any tmux client is currently displaying the session that fired the
  hook. When non-zero, the daemon dispatches the macOS banner but skips
  enqueuing — you don't need a queue entry for the session you're sitting in.
- `printf '\a'` keeps the terminal bell so tmux's window-status-bell still
  flashes the originating window even if you're focused elsewhere.

## 5. Wire up tmux

Append the block below to `~/.tmux.conf`. Markers are intentional —
`sed -i '' '/>>> claudewatch/,/<<< claudewatch/d' ~/.tmux.conf` removes it
cleanly.

```tmux
# >>> claudewatch integration (managed; remove the block between markers to uninstall)
set-environment -g PATH "$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

set-hook -g client-session-changed 'run-shell -b "curl -s --max-time 1 -X POST localhost:7777/sessions/#{session_name}/clear >/dev/null 2>&1 || true"'

set -g status-right '#(cw summary --format=tmux)'

bind N run-shell -b 'next=$(cw next 2>/dev/null); if [ -n "$next" ]; then tmux switch-client -t "$next"; else tmux display-message "claudewatch: no pending sessions"; fi'

bind D new-window -n dash 'watch -n 1 cw'
# <<< claudewatch integration
```

Reload: `tmux source-file ~/.tmux.conf`.

Notes:
- `set-environment -g PATH` is needed so `cw`, `tmux`, and `watch` resolve in
  `run-shell`/status/new-window contexts (they get a minimal PATH otherwise).
- `client-session-changed` was empirically verified on tmux 3.6a to fire
  *after* the switch. `#{session_name}` resolves to the destination session,
  which is what we want (clear the session being switched **to**).
- `run-shell -b` backgrounds the command so its output never pops a viewer
  window.
- Uppercase `bind N` and `bind D` are deliberate — lowercase `n` and `d` are
  default tmux bindings and a common user binding, respectively.

## 6. (Optional) `dev` function integration

If you use a `dev` shell function that wraps `tmux new-session`/git-worktree
creation (e.g., the one in `~/work/useful-code/zsh/.zshrc`), add two curl
calls so the daemon sees sessions even before any hook fires.

After the function's `tmux set-option ... @mode "$mode"` line:

```bash
curl -s --max-time 2 -X POST localhost:7777/sessions \
  --data-urlencode "session=$name" \
  --data-urlencode "project=${repo_name:-ungrouped}" \
  --data-urlencode "worktree=$work_dir" >/dev/null 2>&1 || true
```

In the `dev kill <name>` branch, after `tmux kill-session -t "$name"`:

```bash
curl -s --max-time 2 -X DELETE "localhost:7777/sessions/$name" >/dev/null 2>&1 || true
```

Skip this entirely if you don't use such a function — the daemon falls back
gracefully and registers sessions on their first hook event.

## 7. (Optional) Coordinate with other notification plugins

If you have another Claude Code notification plugin registered (e.g.,
[`claude-notifications-go`](https://github.com/777genius/claude-notifications-go)),
both systems will fire banners for the same Stop / Notification events.

Two ways to fix the duplication:

**Recommended:** keep the other plugin (it's typically more featureful — keyword categorization, click-to-focus, sounds, webhooks) and silence claudewatch's banners. Edit `~/.config/claudewatch/config.toml`:

```toml
notifications_enabled = false
```

claudewatch keeps everything else (queue, status-line `◆`, dashboard, `prefix+N`); the other plugin owns the banner UX.

**Alternative:** disable the other plugin. In `~/.claude/settings.json`, set its `enabledPlugins` entry to `false`, e.g.:

```json
"enabledPlugins": {
  "claude-notifications-go@claude-notifications-go": false
}
```

claudewatch's banners then stand alone — `❓ project · session "needs your input"` for Notification, `✅ project · session "task complete"` for Stop.

## 8. (Optional) Allow notifications during DND / Focus

macOS doesn't let CLI tools bypass Focus modes without an Apple-granted
"Critical Alerts" entitlement. Two workarounds:

- **System Settings → Notifications → terminal-notifier → Time Sensitive
  Notifications** (toggle on if available).
- **Settings → Focus → [your DND Focus] → Allowed Apps → "+" →
  terminal-notifier**.

If the toggle isn't visible, the per-Focus allowed-apps list is the fallback.

## Uninstall

```sh
cd ~/work/claudewatch
make uninstall
sed -i '' '/>>> claudewatch/,/<<< claudewatch/d' ~/.tmux.conf
tmux source-file ~/.tmux.conf
# Manually revert the Notification/Stop hooks in ~/.claude/settings.json
# Manually revert the curl POST/DELETE in your `dev` function
```

Backups created during install:
- `~/.claude/settings.json.bak.claudewatch-YYYYMMDD`
- `~/.tmux.conf.bak.claudewatch-YYYYMMDD`
- `~/work/useful-code/zsh/.zshrc.bak.claudewatch-YYYYMMDD` (if `dev` was edited)

## Troubleshooting

**Banner shows "Script Editor", click opens Xcode.** `terminal-notifier`
isn't being found by the daemon. Check that `make install` regenerated the
plist (it should include `EnvironmentVariables.PATH`) and that
`terminal-notifier` is on `/opt/homebrew/bin`.

**`prefix+N` opens a popup window with shell output.** Missing `-b` on
`run-shell`. The block in section 5 already has it; if you copied an
earlier version, update it.

**`prefix+N` says "no pending sessions" when the dashboard shows pending.**
`cw` isn't on tmux's PATH. Confirm the `set-environment -g PATH` line is
present and re-source `~/.tmux.conf`.

**Notifications stopped firing after sleep/wake.** launchd should restart
the daemon (`KeepAlive=true`); verify with `launchctl list | grep
claudewatch`. Logs: `~/Library/Logs/claudewatch/stderr.log`.
