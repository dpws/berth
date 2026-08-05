# berth-clipd

Serves this machine's clipboard image to a [berth](https://github.com/dpws/berth)
running somewhere else, so `ctrl+y` there pastes a screenshot from here.

Run it on the machine you are **sitting at**. berth runs on the machine you
have SSH'd **into**.

Since v0.10 it does one more thing in the same direction, and for the same
reason: **`POST /notify` raises a desktop notification on this machine.**
Windows Terminal understands no escape sequence for one — berth tried both
`OSC 9` and `OSC 777` against it and neither produced anything — so a berth on
the far end of an SSH connection has no way to put words on your screen. The
agent is already here, so berth asks it instead.

```sh
curl -X POST --data "api is waiting on you" http://127.0.0.1:8377/notify
```

`X-Berth-Title` sets the heading, `berth` by default. On Windows the toast is
raised through PowerShell, attributed to it — Windows will not show a toast
from an application it has never been told about, and registering one means
writing to the registry and the start menu for the sake of a line of text.
Elsewhere it shells out to `notify-send`. The endpoint takes the same
`-token` as the rest, and it should: it is a door onto your desktop.

## Why it exists

A clipboard can only be read by a process on the machine that owns it. No
terminal or SSH feature carries image bytes: OSC 52, the escape sequence
terminals use for clipboard access, is text-only by specification, and
terminals disable its read direction anyway because a remote host being able to
read your clipboard is a security hole.

So pasting a screenshot into a remote session needs a small helper at your end.
This is it.

```
your machine                          the machine running berth
┌────────────────────┐               ┌──────────────────────────┐
│ Win+Shift+S        │               │ ctrl+y                   │
│   ↓                │               │   ↓                      │
│ clipboard          │               │ GET /image               │
│   ↓                │               │   ↓                      │
│ berth-clipd    │◀── ssh -R ────│ 127.0.0.1:8377           │
│ 127.0.0.1:8377     │─── PNG ──────▶│   ↓                      │
└────────────────────┘               │ path typed into prompt   │
                                     └──────────────────────────┘
```

## Install

### Windows

```powershell
.\install-clipd.ps1
```

Copies the binary to `%LOCALAPPDATA%\berth`, starts it at login, and checks
that it answers. No administrator rights needed, nothing written outside your
user profile. `-Uninstall` reverses all of it.

Options: `-Port <n>`, `-NoStartup`, `-Token <secret>`, `-Source <path to exe>`.

### macOS and Linux

```sh
brew install pngpaste        # macOS only, for reading clipboard images
install -m 0755 berth-clipd ~/.local/bin/
berth-clipd &
```

## Connect

The agent listens on loopback only, so forward the port when you connect:

```sh
ssh -R 8377:localhost:8377 you@yourbox
```

Better, put it in your `~/.ssh/config` here and forget about it:

```
Host yourbox
  HostName 10.0.0.5
  RemoteForward 8377 localhost:8377
```

berth needs no configuration — `clip_agent_url` defaults to
`http://127.0.0.1:8377`.

## Use

Copy an image, then press `ctrl+y` in berth. Both of these work:

- a screenshot on the clipboard (`Win+Shift+S`, `Cmd+Shift+4`)
- an image **file** copied in Explorer or Finder

berth writes the bytes to a file on its own machine and types that path
into the prompt — which is what Claude Code and Codex want, since they read the
file themselves.

## Endpoints

| Path | Response |
| --- | --- |
| `GET /image` | `200` with the image bytes, or `204` when the clipboard holds no image |
| `GET /health` | version and platform, for checking it is alive |

Both accept `X-Berth-Token` when started with `-token`.

```sh
curl -v http://127.0.0.1:8377/image --output shot.png
```

## Security

The agent binds `127.0.0.1` and **refuses to start on any other address without
`-token`**. Keep it that way.

- Bound to a LAN or VPN address, it serves your clipboard to anything that can
  reach the port. If you must, use `-token` and set `clip_agent_token` to match
  in berth's config.
- Even on loopback with an SSH forward, any process on the remote machine can
  read your clipboard while the tunnel is up. That is much narrower than
  `ssh -X`, which exposes your keystrokes and screen contents, but it is not
  nothing. A token closes it.
- berth checks returned bytes against known image magic numbers, so a
  different service answering on a forwarded port cannot be passed off as a
  pasted image.

## How each platform is read

| OS | Mechanism |
| --- | --- |
| Windows | PowerShell `System.Windows.Forms.Clipboard`, run `-Sta` as the clipboard API requires. Falls back to the first image among files copied in Explorer. |
| macOS | `pngpaste -` |
| Linux | `wl-paste` or `xclip`, negotiating an `image/*` MIME target. `xsel` cannot do this — it only handles text. |

## Troubleshooting

**berth says "clipboard agent unreachable (is the ssh -R tunnel up?)"**
The forward is missing or the agent is not running. On the remote machine,
`curl http://127.0.0.1:8377/health` should answer.

**Windows: nothing is served even with an image copied.**
Run the console build in a terminal to see the error:
`%LOCALAPPDATA%\berth\berth-clipd.exe`. The silent build writes its logs
nowhere.

**The port is already in use.**
Start with `-addr 127.0.0.1:9000`, forward `9000` instead, and set
`clip_agent_url` accordingly in berth's config.
