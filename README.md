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
│ BERTH 3      dev │ main  ↑1  ~2 +1  +47/-12            │
│──────────────────┼─────────────────────────────────────│
│  ◐ api    claude │ dpws@host:~/code/api $ claude       │
│   refactor t… 4m │ ❯ refactor the request parser       │
│  ? web    codex  │                                     │
│  ○ dots   bash   │ (live - type straight into it)      │
│──────────────────│                                     │
│ 5h ▓▓▓░░░░░░  28%│                                     │
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
is `berth-clipd`, the clipboard agent that lets `ctrl+v` on the remote machine
paste a screenshot from this one:

```powershell
irm https://raw.githubusercontent.com/dpws/berth/main/install.ps1 | iex
```

On Windows the installer also offers to teach Windows Terminal that
**shift+enter** means a new line rather than "send this now" - it cannot tell
the two apart on its own, and alt+enter is taken there for fullscreen. It asks
first, lists the files it would change, copies each to `<file>.berth-bak`, and
says that comments and formatting in them are not preserved. `-NoTerminalFix`
skips the offer, `-Yes` takes it without asking, and `-Uninstall` removes the
binding again. Decline it and **ctrl+j** does the same job, changing nothing.

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
berth                # open the session list
berth ls             # print the sessions and exit
berth update         # replace this binary with the newest release
berth help           # print the commands
berth -write-config  # drop a default config file to edit
berth statusline     # Claude Code status line hook — see Rate limits
```

### Keys

The list and the terminal take turns owning the keyboard — the rules above and
below the body are lit along whichever half has it. `ctrl+o` switches between
them; while the terminal has focus **every other key goes to the session**,
including `ctrl+c`, `esc` and tmux's own `ctrl+b` prefix. The
exceptions are `ctrl+v` (paste an image) and `ctrl+x` (quit), which berth keeps
for itself so they work without going back to the list first — both are
configurable. `ctrl+v` is worth remapping to `ctrl+y` if you use vim in a
session, since visual block is that key there, and so is a shell's
quoted-insert; `ctrl+x` is worth remapping if you run emacs, which wants it.

| Key | In the session list |
| --- | --- |
| `↑`/`↓`, `j`/`k` | move (the terminal follows the selection) |
| `enter`, `l` | hand the keyboard to the terminal |
| `ctrl+o` | toggle list ⇄ terminal focus |
| `n` | new session — name, kind, start mode, directory |
| `x` | kill the selected session (asks first) |
| `r` | rename the selected session |
| `c` | give the session a colour |
| `shift+↑`/`shift+↓` | move the session up or down the list |
| `K` / `J` | the same, without reaching for the arrows |
| `/` | filter by name |
| `p` | start a session from a preset |
| `P` | save the selected session as a preset |
| `,` | settings — edit the config without leaving berth |
| `ctrl+v` | paste an image into the focused session |
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

If the copy does not arrive, something between berth and the clipboard is
refusing OSC 52. Two things can be, and `berth doctor` names whichever it is.
**Your terminal**: most accept it, some ask you to turn it on (kitty's
`clipboard_control`, xterm's `allowWindowOps`, iTerm2's "applications may
access the clipboard"). **tmux, if you started berth inside it**: on the
default `set-clipboard external` tmux will set the clipboard for its own copies
and drop an application's, so nothing arrives —

```sh
tmux set -g set-clipboard on
```

Failing both, **`m`** hands the mouse back to the terminal entirely, at the cost
of clicking rows until you press it again; `"mouse": false` starts that way.

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
  "paste_image_key": "ctrl+v",
  "quit_key": "ctrl+x",
  "clip_agent_url": "http://127.0.0.1:8377",
  "clip_agent_token": "",
  "hide_usage": false,
  "hide_agent_status": false,
  "hide_task": false,
  "hide_agent_age": false,
  "show_host": false,
  "notify_bell": false,
  "notify_desktop": false,
  "notify_waiting": true,
  "notify_taskbar": false,
  "notify_idle": false,
  "check_updates": true,
  "hide_window_title": false,
  "usage_refresh_seconds": 30,
  "hide_git_bar": false,
  "git_refresh_seconds": 5
}
```

`session_options` are `tmux set-option` arguments applied to sessions
berth creates — see *Tuning tmux* below. `mouse on` is there by default:
berth forwards the wheel into the pane regardless, and without it nothing acts
on it, so an agent that does not ask for mouse reporting — Codex does not —
cannot be scrolled at all. With it, tmux scrolls its own scrollback for those
and hands the wheel to agents that want it. Set it to `[]` for none. `claude_command` is what a
"claude" session runs — point it at
`claude --model opus` or a wrapper script if you like. `hide_status_bar` turns
tmux's status line off in sessions berth creates, since the sidebar already
says which session you are in; sessions created elsewhere are left alone.
`hide_agent_status`, `hide_task` and `hide_agent_age` control the indicator,
the task line and the time on the end of it — see *What each session is doing*. `quit_key` quits from anywhere, including while a session has the keyboard;
set it to `""` to hand the key back to your sessions, leaving `ctrl+o` then `q`
as the way out. `hide_window_title` stops berth naming the selected session in
the terminal's title bar — by default the tab reads `api (claude) — berth` and
follows the cursor, so berth is findable in a row of terminal tabs instead of
looking like one more anonymous shell. `usage_refresh_seconds` is how often the rate
limit block is re-read.

## Starting and resuming sessions

`n` opens the new session form: a name, a kind, how it should start, a colour,
and a directory. The first row is a way *into* the presets — enter on it lists
what you have saved and fills the rest of the form from one — and the last is a
tick that saves the session you are about to create as a preset of its own.

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
way to move one. **`shift+↑` and `shift+↓` move the selected session up and
down**, and `K` and `J` do the same without leaving the home row. berth
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

Presets carry a colour too, so a session started from one arrives already
marked. Choosing one opens the new session form already filled in, rather than
starting it outright: the preset saves the typing, not the last look before something
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
│ ⠹ api               claude │
│   rewrite the parser    2m │
│ ? web                codex │
│   approve running: rm … 9m │
│ ○ docs              claude │
│   fix the install sect… 4h │
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

**The terminal title carries the tally**, so a tab sitting in the background
says what your agents are up to without you having to look:

```
?2 ⠋1 ○3 api (claude) — berth
```

The glyphs are the list's own — `?` blocked on you, dots working, `○` idle —
so the tab and the sidebar say the same thing in the same vocabulary. A state
nothing is in is left out rather than shown as a zero, and with nothing to
count the title is just the session. Waiting leads, since a tab bar truncates
from the end and that is the number you would mind losing.

The dots stand still up there. A title is rewritten with an escape sequence
rather than redrawn, so animating it would mean writing the whole title ten
times a second for a glyph nobody is looking straight at. Set
`"hide_window_title": true` to keep the tab out of it entirely, or
`"hide_agent_status": true` to stop berth watching the agents at all, which
takes the tally with it.

**Where this comes from.** Claude Code writes a status file per process at
`~/.claude/sessions/<pid>.json`, and that `<pid>` is the same one tmux reports
for the pane, so the match is exact rather than guessed. Its own vocabulary is
`busy`, `waiting`, `idle` and `shell`, which is what the glyphs show; when it
says what it is waiting for, that replaces the task on the second line. Codex
records `task_started` and `task_complete` in its session rollout, which gives
working-or-not honestly, but it logs no pid — so berth matches it by working
directory. Codex also leaves rollouts that never receive a prompt and keeps
writing to them, so berth prefers a rollout that has one over one that is
merely newer; two Codex sessions genuinely in use in the same directory can
still be confused for each other.

An agent session keeps its task row even when berth has nothing to put in it,
so the sessions below do not move every time it learns or forgets something.

**The task is the last thing you asked**, not the session's title. Both agents
also record a title, but it is written once from the opening request and never
updated, so in a session that has moved on to its third piece of work it is
simply wrong. berth tracks the newest prompt instead, and falls back to the
title only when there is no prompt yet.

**How long it has been doing that** sits on the right of the same row: `12m`
beside what a session is waiting for says it has been blocked on you since
before lunch, not that it has just asked. It is the time in the *current* state
— since the turn began for a session that is working, since it ended for one
that is idle — rather than the time since the agent last wrote anything down,
which for a session mid-turn is always "now". Seconds, then minutes, then hours,
then days, rounded to whichever it is on.

**Claude Code writes that status file when its status changes and at no other
time.** There is no heartbeat, so how long ago it was written says nothing
about whether the session is alive: a `busy` from an hour ago is an agent an
hour into a turn, and an `idle` from this morning is a session that has been
sitting at the prompt since. Whether the *process* is still there is the only
question worth asking, and berth asks it directly — a killed agent leaves its
file behind saying `busy`, and that file is dropped because its pid has gone,
not because it is old.

Ageing those records out instead, which berth used to do after ten minutes, got
both cases wrong: a session an hour into a turn — the one you most want the
list to be right about — dropped out and showed the same hollow circle as an
idle one, and a long-idle session quietly lost its task line.

Codex is the other way round and keeps the clock. Its rollout is written to all
through a turn, so one that has gone quiet really has stopped — and since Codex
records no pid, there is nothing to ask instead. A turn with no end recorded is
believed for ten minutes there.

Set `"hide_agent_status": true` to drop the indicator and the title marker,
`"hide_task": true` to keep the indicator and drop the second line, or
`"hide_agent_age": true` to keep the line and drop the time from it.

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

## The bar above the session

The top of the window is split the same way the body is. Over the list sits
berth's own name, how many sessions there are and which build you are on; over
the session sits the branch its directory is on and what has changed there.

```
 BERTH 3      dev │ main  ↑1 ↓2  ~3 +1 -1  +47/-12      ~/code/api
──────────────────┼───────────────────────────────────────────────
```

| | |
| --- | --- |
| `main` | the branch, or the commit when the head is detached |
| `↑1` `↓2` | commits your branch has that its upstream does not, and the other way round |
| `~3` `+1` `-1` | files modified, added (untracked included) and deleted |
| `+47/-12` | lines added and removed, **unstaged only** — what is still in front of you |

Green is what you have, red is what you have not: commits you are ahead by and
lines you have written are green, commits you are behind by and lines you have
cut are red. Yellow is where you are and what you have touched — the branch and
the files you have changed.

Anything at zero is left out, so a clean branch level with its upstream shows
just its name. The counts are there to be noticed, and noticing them depends on
their being rare.

The counts follow the cursor: each session has its own directory, so moving
down the list re-reads whichever repository that session is sitting in. A
directory that is not a repository keeps the row and shows only its path —
the row does not come and go, because a bar that appeared and vanished as the
cursor moved would resize the session underneath it every time.

Reading is one `git status` and one `git diff --numstat`, every five seconds
and on every move of the cursor, both with `GIT_OPTIONAL_LOCKS=0` so berth
never takes the index lock out from under an agent running its own git in the
same directory. `"git_refresh_seconds"` changes the interval and
`"hide_git_bar": true` silences the half over the session — the half over the
list stays either way, so hiding it gives no room back.

## berth doctor

berth is a thin thing on top of tmux, a terminal and an agent, and it only works
as well as they are set up. The failures are quiet ones - shift+enter submits a
half-written prompt, colour is rounded to 256, a session never learns it lost
the keyboard - and none of them look like a berth bug.

```sh
berth doctor          # say what is set and what each setting costs
berth doctor --fix    # apply the ones berth can
```

It checks tmux (whether modified keys are passed on, focus is reported, the
mouse is taken, colour is capped, how long escape waits, and — when berth is
running inside tmux — whether a copy is passed on), the terminal (whether
it can tell shift+enter from enter, and whether it accepts a copy over OSC 52),
and the agents (whether Claude Code is pointed at berth's status line).

Without `--fix` it changes nothing and prints the command for each finding, so
the work can be done by hand. With it, berth applies what it can: tmux options
go to the running server *and* to `~/.tmux.conf`, so they take effect now and
survive a restart. **Any file berth edits is copied to `<file>.berth-bak`
first**, once - not before each change, so the copy is the file as it was before
berth had ever touched it. Its own additions go under a `# added by berth
doctor` line, and a setting already there is left alone rather than repeated.

berth also runs the checks when it starts and offers to put things right:

```
  berth doctor  4 to look at

  ▸ tmux is dropping modified keys (extended-keys off)  tmux · broken
    tmux is not reporting focus  tmux · could be better
    tmux is leaving the mouse alone  tmux · could be better
    kitty may be refusing copies  terminal · could be better

  f fix what berth can · s never ask again · esc not now
```

`esc` is not now and changes nothing; `s` records those checks in
`doctor_skipped` so they are not raised again. `"hide_doctor": true` turns the
startup check off entirely, and clearing `doctor_skipped` brings back anything
you told it to forget. The prompt never appears over something else you have
open.

## Multi-line prompts, and telling tmux to say more

Agents take more than one line of instruction, and **shift+enter** is how you
give them one without sending what you have written. berth also accepts
**alt+enter** and **ctrl+j**, which mean the same thing: all three reach the
session as an escape and a return, which is what Claude Code and Codex read as
a line break rather than a prompt.

**shift+enter needs the terminal to be able to say it.** Terminals send the
same byte for enter and shift+enter unless they speak the keyboard protocol
kitty defined. kitty, ghostty, WezTerm, foot and recent iTerm2 can; Windows
Terminal and most others cannot, and nothing downstream can recover a
distinction that was never sent. `berth doctor --keys` says which yours is:
press shift+enter and enter, and see whether they read differently.

**If berth runs inside tmux**, tmux also has to pass the difference on:

```sh
tmux set -s extended-keys on          # this session only
echo 'set -s extended-keys on' >> ~/.tmux.conf   # and every one after
```

That only applies when tmux is between your terminal and berth. Run berth
directly - the usual way, over SSH - and tmux is downstream of your keyboard,
so the setting has nothing to do with it.

**Windows Terminal takes alt+enter for fullscreen**, so there ctrl+j is the one
to use, or let `install.ps1` bind shift+enter for you - it offers, and says what
it would change before it does.

**While you are in there**, tmux also caps colour at 256 unless told the
terminal underneath it can do better:

```sh
tmux set -as terminal-features ',*:RGB'
```

berth reads what tmux advertises and draws within it, so without this its
colours are quietly rounded to the nearest of 256 rather than being wrong -
but the meters and the branch read better with the full range.

## Rate limits

Select an agent session and the block under the list says how much of its rate
limit is gone:

```
┌──────────────────────────┬─────────────────────────
│ BERTH 3              dev │ main  ↑1  ~2 +1
│──────────────────────────┼─────────────────────────
│  ● api            claude │
│  ○ web            codex  │
│──────────────────────────│
│ 5h   ▓▓░░░░░░  28%  2h 5m│
│ week ▓▓▓▓▓░░░  61% 2d 13h│
└──────────────────────────┴─────────────────────────
```

**The two agents are not equally well served here, and the block says which
you are looking at.**

**Codex** numbers are exact. The Codex CLI records the server's own answer —
percentage used, window length, reset time — in its session logs at
`~/.codex/sessions`, and berth reads them. Whatever your plan reports is what
you see, whether that is a 5-hour window, a weekly one, or both.

Codex meters some models against separate buckets, so a session that switches
model starts reporting against a different limit. berth keeps every bucket and
labels each by its limit rather than showing only whichever was written last —
a bucket you have barely touched is otherwise the newest thing on disk, and
reads as an empty meter while your real quota is most of the way gone:

```
 codex 5h   ▓▓▓▓▓░░░░░░   34%
 codex week ▓▓▓▓▓▓▓▓░░░   70%
 spark 5h   ░░░░░░░░░░░    0%
 spark week ░░░░░░░░░░░    0%
```

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
only refresh while an agent is running: neither writes anything down while it
sits idle, so what you are looking at is the last reading rather than a live
one.

berth used to say how old that reading was, and no longer does. The line was
never on screen at a useful moment. Codex writes its rollout only while it is
running, so a Codex reading is stale within twenty minutes of a turn ending and
the note was permanent furniture. Claude Code runs its status line every turn,
so a Claude reading is seconds old for as long as you are using Claude and the
note appeared only once you had stopped. What it was there to qualify has moved
onto the rows below, where it does not go out of date.

**How long each window has left rides on its own row, after the percentage** —
`28%  2h 5m`, `61% 2d 13h`. Per window, because a block metering two of them
has to say which one it means: the soonest boundary is rarely the one you are
up against, and a line underneath saying `resets 14:20` while the weekly window
sat at 85% answered a question nobody asked.

It counts down rather than naming a time, since `2h 5m` is the number you want
and `14:20` is one you have to subtract the clock from — and a weekly window
lands days out, where a bare clock time reads as this afternoon. At most two
units, so the column stays a few cells wide. A reading going stale does not
make it wrong: it is worked out from a fixed moment the agent was told about,
not from anything berth is measuring. A boundary already behind us shows
nothing — that window has rolled over, and the agent has simply not run since
to say what the next one is.

**The percentage sits against its meter**, not out at the edge — a number a
hand's width from the bar it describes reads as a third thing on the row rather
than the same fact twice — and the times keep the right edge, in a column of
their own so they stay in line with each other. Every meter berth draws shares
those columns, the machine's below as well as the plan's, so the sidebar reads
as one column of figures rather than two blocks that nearly agree.

On a narrow sidebar the meter goes first and the figures outlast it: a bar is a
picture of a number that is already on the row. With no bar to sit against, the
percentage joins the times on the right rather than trailing its label across an
empty row. That is the whole block for either agent: meters, and what each
window has left, with nothing underneath.

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

## The machine

`"show_host": true` — or **Host meters** in `,` — adds a second block under the
limits, in the same shape, for the box berth is running on:

```
│────────────────────────────│
│ 5h   ▓░░░░░░░░░   8% 2h 57m│
│ week ▓▓▓▓▓▓▓▓▓░  88% 2d 12h│
│────────────────────────────│
│ cpu  ▓▓▓▓▓▓▓░░░  67%   2.69│
│ mem  ▓▓▓▓▓▓░░░░  55%   7.1G│
│ disk ▓░░░░░░░░░   8%   406G│
│────────────────────────────│
```

It reads the same way as the rate limits above it, and is drawn to the same
columns: the bar and the percentage against it are what is **gone**, and the
figure on the right is what is **left** — memory and disk free, and for `cpu`
the load average the percentage was worked out from. Anything past 90% turns
red.

`cpu` is the one-minute load average divided by the number of processors, so
100% means they are all busy and it can go past that — load counts what is
waiting to run as well as what is running, and a machine three deep reads as
300%. That is a different question from "how busy is this core right now", and
the load beside the bar is there so the figure stays checkable against `uptime`.

`mem` counts what could still be handed to a program that asked, not what is
unallocated: both systems spend every spare page on cache and give it back on
demand, so `MemFree` on Linux and `Pages free` on macOS read as nearly nothing
on a machine that has been up a while, while most of it is available. `disk` is
the root filesystem, and its percentage is worked out the way `df` works it out
— against what is used plus what you could still write, rather than the whole
device, so berth's figure and `df`'s agree.

It is off by default because berth is a session list first. Turned on, it is
read straight from the kernel's own accounting — `/proc` on Linux, `sysctl` and
`vm_stat` on macOS — at most every two seconds, whatever the session list is
polling at. Nothing is sent anywhere and nothing needs privileges. A number the
machine will not give up leaves its row out rather than drawing an empty bar,
and a system berth has no reader for shows no block at all.

## Being told

Berth watches what every agent is doing anyway, which means it knows the moment
one of them starts needing you. Four switches turn that into something you can
hear from another window — all of them in `,` as well as the file:

```json
{
  "notify_bell": true,
  "notify_desktop": true,
  "notify_waiting": true,
  "notify_idle": false
}
```

The first two are how, and either or both can be on. **`notify_bell`** rings
the terminal — universally supported, and most terminals mark the tab as having
activity even with the audible bell off, though a beep says nothing about
*which* session. **`notify_desktop`** asks the terminal to raise a real
notification, naming it: `api is waiting on you`. That is OSC 9, so like a copy
it is your terminal that raises it — **berth running on a box across an SSH
connection notifies the machine you are sitting at**. kitty, WezTerm, iTerm2,
Windows Terminal and foot understand it; terminals that do not ignore it
silently, which is why both can be on at once.

**`notify_taskbar`** is the third, and the odd one out: it is not an
announcement but a state. While any session is waiting on you the terminal is
asked to mark its place in the taskbar, and the mark stays until the last one
stops waiting — so a moment you were away for is still on screen when you come
back, which neither a bell nor a toast can manage. It is cleared when berth
quits, since nothing else on the machine knows to.

That one is ConEmu's `OSC 9;4`, and it is the sequence that actually works
where the notification protocols do not. **Windows Terminal raises nothing for
`OSC 9` or `OSC 777`** — both were tried against it and neither produced a
toast — but it took ConEmu's extensions, so the taskbar colours and stays
coloured. On a terminal that has never heard of it, the sequence is dropped in
silence like the rest.

The other two are when. **`notify_waiting`** is a session blocked on a question
or a permission prompt — the one that is actually costing you time, and on by
default so that switching on a bell rings for something. **`notify_idle`** is a
turn that finished, which fires more often and for sessions you were not
waiting on. Neither does anything on its own: with no bell and no desktop
notification there is no way of saying it, and berth stays quiet.

**A bell is only as loud as your terminal makes it.** berth sends the signal;
what happens to it is a terminal setting, and the useful ones are visual rather
than audible. In Windows Terminal, `"bellStyle": "all"` under
`profiles.defaults` flashes the taskbar icon as well as playing the sound —
the default, `"audible"`, is a beep and nothing else. kitty has `bell_on_tab`
and `visual_bell_duration`, iTerm2 has a "Flash visual bell" checkbox. berth
cannot check any of these, let alone set them: over SSH they live on the
machine you are sitting at, not the one berth is running on.

Only changes are announced. berth's first reading of a session teaches it where
that session stands; otherwise starting berth beside three idle sessions would
ring three times for news hours old, and a session sitting at a permission
prompt would announce itself again every couple of seconds. Only work turning
into idle counts as finished, so a session going quiet after answering you is
not reported as having done something. Several sessions moving at once ring
once between them and raise one notification each.

## Pasting images

**ctrl+v** pastes an image into the focused session. berth takes that key from
the session to do it, so a shell loses quoted-insert and vim loses visual
block; `"paste_image_key": "ctrl+y"` gives it back.

berth also watches for a paste that arrives empty. That is what a terminal
sends when its own paste key was pressed over a clipboard holding no text — an
image, usually — and it is the one case the terminal cannot serve and berth
can. It matters most on Windows Terminal, which keeps `ctrl+v` for itself and
never passes the key on, so the empty paste is the only sign berth gets that
you asked for one.

`ctrl+v` puts an image in front of the agent in the focused session. Terminals
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
   `ctrl+v` from a Windows or macOS workstation.
3. **A drop folder** (`~/berth-drop` by default), taking the most recently
   modified image. Always available, needs no setup:

   ```sh
   scp shot.png pi:berth-drop/     # from your laptop
   ```

   then `ctrl+v` in the session. The folder is created on first use.

Clipboard images are written to `~/.cache/berth/images`, pruned to the 20
most recent. Paths containing spaces are copied to a clean name first so the
prompt does not split them. When nothing is found, the status line says
everything that was tried rather than failing silently.

Rebind with `"paste_image_key"`. It is one of only two keys berth keeps
for itself while a session has focus — the other is `ctrl+o`.

### berth-clipd: the clipboard of the machine you are sitting at

If `ctrl+v` reports **"the tunnel is open but berth-clipd is not running on the
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
│ clipboard          │               │ ctrl+v                   │
│   ↓                │               │   ↓                      │
│ berth-clipd        │◀── ssh -R ────│ GET 127.0.0.1:8377/image │
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

berth's `ctrl+v` does not need this — it reads the clipboard in its own
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
