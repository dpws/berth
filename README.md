# berth

[![tests](https://github.com/dpws/berth/actions/workflows/ci.yml/badge.svg)](https://github.com/dpws/berth/actions/workflows/ci.yml)
[![release](https://github.com/dpws/berth/actions/workflows/release.yml/badge.svg)](https://github.com/dpws/berth/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/dpws/berth.svg)](https://pkg.go.dev/github.com/dpws/berth)

A terminal UI for juggling Claude Code, Codex and plain shell sessions. tmux
does the heavy lifting underneath; berth gives it a session list on the left
and the selected session's **live terminal** on the rest of the screen.

Every session gets a berth: a slot in the list you can dock into and leave
again, still running when you come back.

```
┌──────────────────┬─────────────────────────────────────┐
│ BERTH 3          │ dpws@host:~/code/api $ claude       │
│──────────────────│ ❯ refactor the request parser       │
│▸ ● api    claude │                                     │
│  ○ web    codex  │ (live - type straight into it)      │
│  ○ dots   bash   │                                     │
│──────────────────│                                     │
│ n new  x kill    │                                     │
└──────────────────┴─────────────────────────────────────┘
 ctrl+o back to list  ·  keys go to api
```

Quit berth and every session keeps running — they are ordinary tmux
sessions, reachable with `tmux attach` like any other.

## Install

**Linux and macOS**

```sh
curl -fsSL https://raw.githubusercontent.com/dpws/berth/main/install.sh | sh
```

**Windows** — berth needs a pty and tmux, so it does not run there. What does
is `berth-clipd`, the clipboard agent that lets `ctrl+y` on the remote machine
paste a screenshot from this one:

```powershell
irm https://raw.githubusercontent.com/dpws/berth/main/install.ps1 | iex
```

**Go**

```sh
go install github.com/dpws/berth@latest
```

The shell installer takes `VERSION`, `BERTH_INSTALL_DIR`, `BERTH_CLIPD=1` to
add the agent locally, and `BERTH_DRY_RUN=1` to see what it would do. It
verifies the release checksum, never uses sudo, and writes only to the target
directory. Read it first if you would rather not pipe a script into a shell —
it is short, and `sh install.sh` works just as well after downloading.

### From source

```sh
make build && make install         # to ~/.local/bin
sudo make install PREFIX=/usr/local
make uninstall
```

Or `./scripts/install-from-source.sh`, which does the same without make.
Requires Go 1.24+ and tmux 3.0+.

## Use

```sh
berth              # launch the TUI
berth ls           # print the session list and exit
berth -write-config  # drop a default config file to edit
```

### Keys

The list and the terminal take turns owning the keyboard. `ctrl+o` switches
between them; while the terminal has focus **every other key goes to the
session**, including `ctrl+c`, `esc` and tmux's own `ctrl+b` prefix.

| Key | In the session list |
| --- | --- |
| `↑`/`↓`, `j`/`k` | move (the terminal follows the selection) |
| `enter`, `l` | hand the keyboard to the terminal |
| `ctrl+o` | toggle list ⇄ terminal focus |
| `n` | new session — name, kind (claude/codex/shell), directory |
| `x` | kill the selected session (asks first) |
| `r` | rename the selected session |
| `/` | filter by name |
| `ctrl+y` | paste an image into the focused session |
| `R` | refresh now |
| `?` | help |
| `q` | quit — sessions keep running |

Mouse: click a row to select it, click again to hand it the keyboard, wheel to
scroll the list. Over the terminal, clicks and the wheel go to the session.
Set `"mouse": false` in the config to give the outer terminal its native text
selection back.

## Config

Optional, at `~/.config/berth/config.json`:

```json
{
  "claude_command": "claude",
  "codex_command": "codex",
  "shell_command": "/bin/bash",
  "default_dir": "/home/you",
  "sidebar_width": 28,
  "refresh_millis": 2000,
  "hide_status_bar": true,
  "mouse": true,
  "session_options": ["mouse on"],
  "image_drop_dir": "/home/you/berth-drop",
  "paste_image_key": "ctrl+y",
  "clip_agent_url": "http://127.0.0.1:8377",
  "clip_agent_token": ""
}
```

`session_options` are `tmux set-option` arguments applied to sessions
berth creates — see *Tuning tmux* below. `claude_command` is what a
"claude" session runs — point it at
`claude --model opus` or a wrapper script if you like. `hide_status_bar` turns
tmux's status line off in sessions berth creates, since the sidebar already
says which session you are in; sessions created elsewhere are left alone.

## Pasting images

`ctrl+y` puts an image in front of the agent in the focused session. Terminals
cannot carry image bytes, so what actually happens is that berth finds the
image on disk and types its **path** into the prompt — which is exactly what
Claude Code and Codex want, since they read the file themselves.

It looks in three places, in order:

1. **The system clipboard of the machine berth runs on**, via `wl-paste` or
   `xclip`. `xsel` will not do: it has no concept of MIME targets and only
   returns text. On a headless box this never yields anything, and berth
   says so rather than failing silently.
2. **A remote clipboard**, served by `berth-clipd` on the machine you are
   actually sitting at — see below. This is the one that gives you a real
   `ctrl+y` from a Windows or macOS workstation.
3. **A drop folder** (`~/berth-drop` by default), taking the most recently
   modified image. Always available, needs no setup:

   ```sh
   scp shot.png pi:berth-drop/     # from your laptop
   ```

   then `ctrl+y` in the session. The folder is created on first use.

Clipboard images are written to `~/.cache/berth/images`, pruned to the 20
most recent. Paths containing spaces are copied to a clean name first so the
prompt does not split them. When nothing is found, the status line says
everything that was tried rather than failing silently.

Rebind with `"paste_image_key"`. It is one of only two keys berth keeps
for itself while a session has focus — the other is `ctrl+o`.

### berth-clipd: the clipboard of the machine you are sitting at

A clipboard can only be read by a process on the machine that owns it, and no
terminal or SSH feature carries image bytes — OSC 52 is text-only by spec, and
terminals disable its read direction anyway. So when berth runs on a box you
have SSH'd into, pasting a screenshot needs a small helper at your end.

`berth-clipd` is that helper: it serves the clipboard image over HTTP on
loopback, and you forward the port when you connect.

```
your workstation                      the box running berth
┌────────────────────┐               ┌──────────────────────────┐
│ clipboard          │               │ ctrl+y                   │
│   ↓                │               │   ↓                      │
│ berth-clipd    │◀── ssh -R ────│ GET 127.0.0.1:8377/image │
│ 127.0.0.1:8377     │─── PNG ──────▶│   ↓ path into the prompt │
└────────────────────┘               └──────────────────────────┘
```

Build it for your workstation — it is a separate binary and does not run here:

```sh
make bundle-windows       # dist/windows/ : binaries + installer + its own README
make clipd-windows        # just dist/berth-clipd.exe
make clipd-darwin         # dist/berth-clipd-darwin
make clipd                # this machine
```

Copy `dist/windows/` across and run the installer there:

```powershell
.\install-clipd.ps1
```

It drops the binary in `%LOCALAPPDATA%\berth`, starts it at login, and
checks that it answers — no administrator rights, nothing outside your user
profile, `-Uninstall` to reverse it. Full details in
[`cmd/berth-clipd/README.md`](cmd/berth-clipd/README.md), which ships
inside the bundle.

Then connect with the port forwarded:

```sh
ssh -R 8377:localhost:8377 you@yourbox
```

Or put it in your workstation's `~/.ssh/config` and forget about it:

```
Host yourbox
  HostName 10.0.0.5
  RemoteForward 8377 localhost:8377
```

berth needs no configuration for this — `clip_agent_url` already defaults
to `http://127.0.0.1:8377`. Set it to `""` to skip the agent entirely.

How each platform's clipboard is read:

| OS | Mechanism | Notes |
| --- | --- | --- |
| Windows | PowerShell `System.Windows.Forms.Clipboard` | Also serves the first image among files copied in Explorer. Runs `-Sta`, which the clipboard API requires. |
| macOS | `pngpaste -` | `brew install pngpaste` |
| Linux | `wl-paste` / `xclip` | Same tools as the local path |

**Security.** The agent binds loopback only, and refuses to start on any other
address unless you give it `-token`. Keep it that way: bound to a LAN or VPN
address, it hands your clipboard to anything that can reach the port. Even on
loopback with a forward, any process on the remote box can read your clipboard
while the tunnel is up — narrower than `ssh -X`, which exposes your keystrokes
and screen, but not nothing. `-token` plus `clip_agent_token` closes that.

berth checks the returned bytes against known image magic numbers, so a
different service answering on a forwarded port cannot end up quoted as a
"pasted image".

## Tuning tmux

Two tmux options matter here, and both are **server- or user-wide**, so they
belong in `~/.tmux.conf`:

```tmux
set -g focus-events on   # let programs know when they gain/lose the keyboard
set -g mouse on          # wheel scrolls scrollback, drag selects
```

Reload with `tmux source-file ~/.tmux.conf` (existing clients pick up
`focus-events` on their next attach — inside berth, just move off the
session and back).

**These do nothing on their own inside berth** unless berth forwards
the events, which it now does: `ctrl+o` into a session sends focus-in, leaving
sends focus-out, and clicks and the wheel are translated into the pane's
coordinate space. Focus and mouse reports are only emitted when the session
actually asked for them, so a plain shell never sees stray escape sequences.

If you would rather not change tmux globally, `session_options` applies
options to sessions berth creates and leaves everything else alone:

```json
{ "session_options": ["mouse on", "history-limit 50000"] }
```

`focus-events` cannot go there — it is a server option, not a session one.

If you connect with `ssh -X` and want Claude Code's *own* `ctrl+v` clipboard
paste to work, the tmux server also needs a live `DISPLAY`. tmux caches the
environment from whenever its server first started, so after reconnecting:

```sh
tmux set-environment -g DISPLAY "$DISPLAY"
```

berth's `ctrl+y` does not need this — it reads the clipboard in its own
process, which always has the current environment.

Other settings worth having, none of them berth-specific:

```tmux
set -g history-limit 50000
set -s escape-time 10      # stops esc feeling laggy in vim and Claude Code
set -g base-index 1
```

## How it works

Each visible session is a real `tmux attach-session` client running in a pty
that berth owns. Its output is fed through an in-process terminal emulator
(`charmbracelet/x/vt`), and the resulting cell grid is drawn into the right-hand
region — that is what makes it possible to show a full terminal *beside*
something else instead of letting it own the screen. Keystrokes travel the
other way through the same emulator, so terminal modes like application cursor
keys are encoded the way the session expects.

Sessions berth creates are tagged with tmux user options (`@berth`,
`@berth_kind`) so their kind survives a restart. Sessions you started
yourself show up too, marked `ext`, and are never modified — their kind is
guessed from the running command so a `claude` or `codex` you started by hand
is still colour-coded.

## Hacking

`make test` runs against a **private tmux socket**: the suite sets
`TMUX_TMPDIR` to a scratch directory, so it can never list, disturb, or tear
down the sessions you are working in. Do the same for any manual end-to-end
run, and keep the path short — unix socket paths cap out around 104 bytes:

```sh
TMUX_TMPDIR=/tmp/cmux-e2e tmux new-session -d -s scratch
TMUX_TMPDIR=/tmp/cmux-e2e ./berth
```

## Known limits

- The cursor inside the terminal is drawn as a reverse-video block rather than a
  real hardware cursor, which a sub-region of the screen cannot own.
- `ctrl+b d` inside a focused session detaches that tmux client; berth
  notices and drops back to the list.
- Set `BERTH_LOG=/path/to/log` to trace attaches when something misbehaves —
  a TUI has nowhere else to print.
