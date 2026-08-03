# berth

[![tests](https://github.com/dpws/berth/actions/workflows/ci.yml/badge.svg)](https://github.com/dpws/berth/actions/workflows/ci.yml)
[![release](https://github.com/dpws/berth/actions/workflows/release.yml/badge.svg)](https://github.com/dpws/berth/actions/workflows/release.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A terminal UI for juggling Claude Code, Codex and plain shell sessions. tmux
does the heavy lifting underneath; berth gives it a session list on the left
and the selected session's **live terminal** on the rest of the screen.

Every session gets a berth: a slot in the list you can dock into and leave
again, still running when you come back.

```
┌──────────────────┬─────────────────────────────────────┐
│ BERTH 3          │ dpws@host:~/code/api $ claude       │
│──────────────────│ ❯ refactor the request parser       │
│▸ ◐ api    claude │                                     │
│   refactor the … │ (live - type straight into it)      │
│  ? web    codex  │                                     │
│  ○ dots   bash   │                                     │
│──────────────────│                                     │
│ 5h  ▓▓▓░░░  28%  │                                     │
└──────────────────┴─────────────────────────────────────┘
 ctrl+o back to the list · ctrl+x quit  ·  everything else typed goes to api
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
berth                # launch the TUI
berth ls             # print the session list and exit
berth -write-config  # drop a default config file to edit
berth update         # replace this binary with the newest release
berth statusline     # Claude Code status line hook — see Rate limits
```

### Keys

The list and the terminal take turns owning the keyboard — the lit half of the
rule above the hotkeys shows which one has it. `ctrl+o` switches between them; while the terminal has focus **every other key goes to the
session**, including `ctrl+c`, `esc` and tmux's own `ctrl+b` prefix. The
exceptions are `ctrl+y` (paste an image) and `ctrl+x` (quit), which berth keeps
for itself so they work without going back to the list first — both are
configurable, and `ctrl+x` in particular is worth remapping if you run emacs in
a session, since emacs wants that key for itself.

| Key | In the session list |
| --- | --- |
| `↑`/`↓`, `j`/`k` | move (the terminal follows the selection) |
| `enter`, `l` | hand the keyboard to the terminal |
| `ctrl+o` | toggle list ⇄ terminal focus |
| `n` | new session — name, kind, start mode, directory |
| `x` | kill the selected session (asks first) |
| `r` | rename the selected session |
| `c` | give the session a colour |
| `J` / `K` | move the session down or up the list |
| `/` | filter by name |
| `p` | start a session from a preset |
| `P` | save the selected session as a preset |
| `,` | settings — edit the config without leaving berth |
| `ctrl+y` | paste an image into the focused session |
| drag | select text in the session, copied on release |
| `m` | hand the mouse to your terminal entirely, and take it back |
| `R` | refresh now |
| `?` | help |
| `q` | quit — sessions keep running |
| `ctrl+x` | quit from anywhere, including a focused session |

Mouse: click a row to select it, click again to hand it the keyboard, wheel to
scroll the list. Over the terminal, clicks and the wheel go to the session.

**Selecting text:** drag across the session. The text highlights as you go and
lands on your clipboard when you let go — no modifier, and clicks still switch
sessions. Over SSH it reaches the clipboard on *your* machine, not the remote
one, because berth hands it to your terminal with OSC 52.

berth does the selecting itself rather than leaving it to the terminal, and it
has to. Mouse reporting is all-or-nothing per terminal, not per region: while
berth holds the mouse so you can click a row, the terminal will not select
anywhere. Shift-drag bypasses that in most terminals, but the terminal has no
idea where berth drew the divider, so it happily selects the sidebar and the
session together on every row. berth knows, and selects only the session.

A drag selects; a plain click still goes through to the program in the session,
so agents keep their own mouse handling. The highlight clears when you type or
change session.

If the copy does not arrive, your terminal is refusing OSC 52 — most support
it, some ask you to turn it on (kitty's `clipboard_control`, xterm's
`allowWindowOps`, iTerm2's "applications may access the clipboard"). Failing
that, **`m`** hands the mouse back to the terminal entirely, at the cost of
clicking rows until you press it again; `"mouse": false` starts that way.

## Config

Press **`,`** to edit any of this from inside berth. Changes apply as you make
them, so you can watch the sidebar resize or the meters disappear as you go;
`ctrl+s` writes them to the file, `d` puts one setting back to its default, and
leaving with unsaved changes says so rather than losing them quietly. Two
settings — the tmux status bar and session options — only reach sessions
created afterwards, and the screen says which those are.

The file itself is optional, at `~/.config/berth/config.json`:

```json
{
  "claude_command": "claude",
  "codex_command": "codex",
  "claude_continue_args": "--continue",
  "claude_resume_args": "--resume",
  "codex_continue_args": "resume --last",
  "codex_resume_args": "resume",
  "shell_command": "/bin/bash",
  "default_dir": "/home/you",
  "sidebar_width": 28,
  "refresh_millis": 2000,
  "hide_status_bar": true,
  "mouse": true,
  "session_options": ["mouse on"],
  "image_drop_dir": "/home/you/berth-drop",
  "paste_image_key": "ctrl+y",
  "quit_key": "ctrl+x",
  "clip_agent_url": "http://127.0.0.1:8377",
  "clip_agent_token": "",
  "hide_usage": false,
  "hide_agent_status": false,
  "hide_task": false,
  "check_updates": true,
  "hide_window_title": false,
  "usage_refresh_seconds": 30
}
```

`session_options` are `tmux set-option` arguments applied to sessions
berth creates — see *Tuning tmux* below. `claude_command` is what a
"claude" session runs — point it at
`claude --model opus` or a wrapper script if you like. `hide_status_bar` turns
tmux's status line off in sessions berth creates, since the sidebar already
says which session you are in; sessions created elsewhere are left alone.
`hide_agent_status` and `hide_task` control the indicator and the task line —
see *What each session is doing*. `quit_key` quits from anywhere, including while a session has the keyboard;
set it to `""` to hand the key back to your sessions, leaving `ctrl+o` then `q`
as the way out. `hide_window_title` stops berth naming the selected session in
the terminal's title bar — by default the tab reads `api (claude) — berth` and
follows the cursor, so berth is findable in a row of terminal tabs instead of
looking like one more anonymous shell. `usage_refresh_seconds` is how often the rate
limit block is re-read.

## Starting and resuming sessions

`n` opens the new session form: a name, a kind, how it should start, and a
directory.

**Start mode** decides whether an agent begins fresh or picks up where it left
off. `new` is a clean start; `continue` carries on the most recent conversation
in that directory; `resume` asks which earlier one to take. Shell sessions have
no conversation to carry on, and the row says so. The arguments each mode adds
are configurable — `claude_continue_args` and friends — and they are *appended*
to the configured command, so `"claude_command": "claude --model opus"` becomes
`claude --model opus --continue` rather than losing its options.

**The directory field completes with `tab`**, as a shell would: it extends the
path as far as the candidates agree, and lists them underneath when more than
one fits. `~` is expanded to match, and kept in the text afterwards rather than
being silently rewritten into a longer path. Directories only — this is the
field that says where a session starts — and hidden ones stay out of the way
until you type the leading dot. `↑`/`↓` move between fields, since `tab` is
busy completing.

### Ordering

tmux has no idea of session order — it lists them alphabetically and offers no
way to move one. `J` and `K` move the selected session down and up, and berth
remembers where you put it on the tmux session itself, so the arrangement
survives berth restarting.

A session you have never moved keeps tmux's own order and sits after the ones
you have placed, rather than jumping to the front. Moving with a filter on
swaps with the next session you can *see*, not the next one in the full list.

### Colours

`c` gives the selected session a colour from a small palette. It marks the
session's name and the spinner it shows while working, so a glance at the list
says which project is which.

Waiting and idle keep their own colours regardless: those say what a session is
*doing*, and a colour you chose says which session it *is* — the second should
not be able to drown out the first. Colours are stored on the tmux session as a
palette name rather than a value, so they survive berth restarting and read
correctly on a light terminal and a dark one alike.

### Presets

A session you set up often is worth keeping. `P` saves the selected one as a
preset — its kind, its directory and a name to offer — and `p` lists what you
have saved:

```
┌──────────────────────────────────────────────────────────────┐
│  Presets  2                                                  │
│                                                              │
│  api on claude                           claude  ~/code/api  │
│  web on codex                             codex  ~/code/web  │
│                                                              │
│  enter use · x remove · esc close                            │
└──────────────────────────────────────────────────────────────┘
```

Choosing one opens the new session form already filled in, rather than starting
it outright: the preset saves the typing, not the last look before something
runs. So you can still change the start mode, point it at a different
directory, or rename it before pressing enter. `x` removes one.

Presets live in `~/.config/berth/presets.json`, next to the config rather than
inside it — the config is a flat list of settings, and this is a list of
things. Saving under a name already in use replaces it.

## What each session is doing

Every agent session carries an indicator, and a dim second line saying what it
was last asked to do:

```
┌────────────────────────────┐
│ BERTH 4                    │
│────────────────────────────│
│ ⠹ api               claude │
│   rewrite the parser       │
│ ? web                codex │
│   approve running: rm -rf… │
│ ○ docs              claude │
│   fix the install section  │
│ ○ dots               shell │
└────────────────────────────┘
```

| | | |
| --- | --- | --- |
| spinner | green | the agent is working |
| `?` | red | **the agent is waiting on you** — a question or a permission prompt |
| `○` | white | idle, or for a plain shell, no client attached |

The three are meant to be told apart without colour as well as with it: a
spinner turning means something is happening, a question mark means the session
is stuck until you answer it. The spinner only ticks while something is
actually working, so an idle berth is not redrawing for a glyph that is not
moving.

When anything is waiting, berth also marks the terminal title — `● api (claude)
— berth`, or `●2 …` for two — so a tab sitting in the background tells you a
session is blocked without you having to look. The marker leads the title so it
survives the tab being truncated.

**Where this comes from.** Claude Code writes a status file per process at
`~/.claude/sessions/<pid>.json`, and that `<pid>` is the same one tmux reports
for the pane, so the match is exact rather than guessed. Its own vocabulary is
`busy`, `waiting`, `idle` and `shell`, which is what the glyphs show; when it
says what it is waiting for, that replaces the task on the second line. Codex
records `task_started` and `task_complete` in its session rollout, which gives
working-or-not honestly, but it logs no pid — so berth matches it by working
directory, and two Codex sessions in the same directory can be confused for
each other.

**The task is the last thing you asked**, not the session's title. Both agents
also record a title, but it is written once from the opening request and never
updated, so in a session that has moved on to its third piece of work it is
simply wrong. berth tracks the newest prompt instead, and falls back to the
title only when there is no prompt yet.

A killed agent leaves its status file behind saying `busy`, so anything not
written to for ten minutes is ignored rather than shown as working forever.
Set `"hide_agent_status": true` to drop the indicator and the title marker, or
`"hide_task": true` to keep the indicator and drop the second line.

Messages along the bottom — `created api`, `copied 2 lines`, a tmux error —
hold for a few seconds and then fade out, giving the row back to the key hints.
Errors hold twice as long as notices, on the grounds that missing one costs
more. Nothing redraws at that rate when the footer is quiet.

## Updating

`berth update` replaces the running binary with the newest release. It checks
the download against the checksums the release publishes before trusting it,
writes the new binary beside the old one and renames it over the top — which
is atomic, and allowed even for the binary you are running, since the process
keeps the file it started from until it next starts.

The header carries the build you are on, and marks it when the daily check has
found a newer one:

```
 BERTH 3              v0.3.0     up to date
 BERTH 3             ↑v0.3.0     a newer release is out — shown in red
 BERTH 3             v0.3.0+     built from source, ahead of that tag
```

The two markers say opposite things and can never appear together, since a
build from source is never told it is out of date.

That check is the only request berth makes on its own. It sends nothing but
the question, caches the answer for a day, says nothing at all when it fails,
and never installs anything — `berth update` is what acts on it. Turn it off
with `"check_updates": false`, or from the settings screen.

A build from source is never told it is out of date. `git describe` marks
those, and such a build is usually ahead of the newest tag rather than behind
it; being nagged to downgrade would be worse than being told nothing.

## Rate limits

Select an agent session and the block above the legend says how much of its
rate limit is gone:

```
┌──────────────────┬─────────────────────────
│ BERTH 3          │
│──────────────────│
│▸ ● api    claude │
│  ○ web    codex  │
│──────────────────│
│ 5h   ▓▓▓░░░  28% │
│ week ▓▓▓▓▓░  61% │
│ resets 14:20     │
│──────────────────│
│ n new  x kill    │
└──────────────────┴─────────────────────────
```

**The two agents are not equally well served here, and the block says which
you are looking at.**

**Codex** numbers are exact. The Codex CLI records the server's own answer —
percentage used, window length, reset time — in its session logs at
`~/.codex/sessions`, and berth reads the most recent one. Whatever your plan
reports is what you see, whether that is a 5-hour window, a weekly one, or
both.

**Claude** needs one thing wired up first, and then it is exact too.

Claude Code pushes your real 5-hour and weekly windows — the same ones `/usage`
draws — to whatever command you configure as its status line. Nothing is
fetched and no credentials are involved: Claude Code hands the numbers to a
program you chose. Point it at berth, in `~/.claude/settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "berth statusline"
  }
}
```

berth records the limits and prints a short line of its own
(`Opus 5 · ctx 8% · 5h 24% · week 41%`). **Already have a status line you
like?** Put it after `--` and berth passes the payload straight through, so you
keep your own bar:

```json
{ "statusLine": { "type": "command", "command": "berth statusline -- ~/.claude/my-statusline.sh" } }
```

Until that is set up, Claude sessions show `run berth statusline` in place of
the bars. **berth does not estimate.** It could add up the tokens in Claude
Code's transcripts and call the result a rate limit, but that number is tokens
spent rather than a share of your plan, and there is no published ceiling to
turn one into the other — so it would be a figure that looks authoritative and
is not. A blank that tells you how to fill it is worth more.

Two things to know. `rate_limits` is only sent to Claude.ai subscribers, and
only after a session's first response — so a brand new session shows nothing
until it has answered once, and API-key users never get it. And the numbers
only refresh while a Claude session is running, so a reading older than twenty
minutes is labelled `as of 14:05` rather than passed off as live.

**What berth will not do.** Claude Code's `/usage` reaches an internal endpoint
with the Claude Code login token. Driving that from another program is
automated access to a subscription without an API key, which [Anthropic's terms
don't allow](https://www.anthropic.com/legal/consumer-terms), so berth doesn't
— the status line above is the sanctioned way to the same numbers. An API key
would make polling allowed but would not answer the question: API keys have no
5-hour or weekly windows at all, being metered per minute and against a monthly
spend cap, and reading even that needs an Admin key, which is not issued to
individual accounts.

The block costs four rows and is read from disk, so it polls far slower than
the session list: `usage_refresh_seconds` (default 30). Reads are incremental —
only the tail of each transcript is parsed after the first pass. Set
`"hide_usage": true` to turn the whole thing off.

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

If `ctrl+y` reports **"the tunnel is open but berth-clipd is not running on the
machine at the other end"**, the forward is fine and the far end is not
answering. Check the agent is running there, and that it is on the same port
and the same loopback family your `-R` points at — writing the forward as
`localhost` resolves it on the machine running `ssh`, which is `::1` first on
some systems. berth-clipd serves both loopback families, so `127.0.0.1` in the
forward is the safe spelling either way. **"nothing is listening at ..."** is
the other half: no tunnel at all.

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
ssh -R 8377:127.0.0.1:8377 you@yourbox
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
- Claude rate limits need `berth statusline` wired into Claude Code's
  `statusLine` setting; without it that block says so rather than guessing.
  Codex limits are always the server's own numbers. See *Rate limits*.
- Codex sessions are matched to their logs by working directory, since Codex
  records no process id; two Codex sessions in the same directory can be
  reported as each other.
- The terminal title berth sets survives it quitting — there is no way to ask a
  terminal what its title was before. Most shells rewrite it at the next
  prompt; if yours does not, `"hide_window_title": true` leaves it alone.

## License

MIT - see [LICENSE](LICENSE).
