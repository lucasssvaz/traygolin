# Usage

## Window

The layout follows Trayscale:

- **Header switch** connects and disconnects. It stays where you put it while
  the tunnel is starting or stopping, and a spinner shows progress.
- **Account menu** in the sidebar header switches between saved accounts.
- **Sidebar** shows **Not Connected** while offline. When connected it shows
  **This Device** and a **Sites** section with one page per site.

| Page | Contents |
| --- | --- |
| Not Connected | What is missing before you can connect: the CLI, one-time setup, or a login. Otherwise Organization and Exit Node dropdowns and a Connect button. |
| This Device | Status, tunnel address, sites summary, CLI, tunnel and server versions, account, server address, and Organization and Exit Node dropdowns. |
| Site | Connected, path (Direct, Relay or Local), latency, last handshake, endpoint, peer address, whether it is your exit node. |

The main menu has Organization and Exit Node submenus, Add Account, Log Out, Preferences, View Logs, Check for CLI Updates, About and
Quit. `Ctrl+Q` quits and `Ctrl+,` opens Preferences.

## Tray

The icon is dimmed when disconnected, solid when connected, and shows an
arrow badge while an exit node is in use. The menu:

- status line
- Connect / Disconnect
- Account: your accounts, Add Account…, Log Out
- Organization: your organizations
- Exit Node: None and your organization's exit nodes
- Show Traygolin, Preferences…, Quit

## Preferences

| Page | Options |
| --- | --- |
| General | Start at Login, Connect at Start, Show Tray Icon, Status Refresh Interval, Passwordless Connect setup |
| DNS | Enable Aliases (Override DNS), DNS Over Tunnel, Primary/Secondary Upstream DNS, Match Domains, Reset DNS |
| Routing | Prefer Local Routes, Exit Nodes Take Precedence Over Resources, Holepunch, Disable Relay, MTU |
| Accounts | Saved accounts, Use, Log Out, Add Account… |

DNS and routing options are the official clients' options and are saved in
the CLI's own `config.json` with `pangolin config set`. Holepunch, Disable
Relay and MTU are Traygolin settings passed to `pangolin up`; they apply on
the next connection.

## Exit nodes and organizations

Traygolin lists your organizations and their exit nodes from the Pangolin
server with the CLI's saved session. These are read-only requests, and the
session token is read from the CLI's account file for each request and never
logged. The lists refresh when you switch account or organization, and every
minute otherwise.

Picking an entry runs `pangolin select org` or `pangolin select exit-node`.
A running tunnel switches immediately; otherwise the choice is saved for the
next connection, like the official clients.

Exit nodes need Pangolin 1.24 or newer on the server and an exit node
resource in the organization. Without one, the exit node controls are
greyed out and say why.

## Status refresh

Traygolin reads status from the running tunnel's local socket, which is fast
and needs no privileges. The default interval is 5 seconds (minimum 1). While
connecting or switching, it checks every second.

```bash
gsettings set io.github.lucasssvaz.Traygolin poll-interval 10
traygolin --poll-interval 2      # this session only
```

If the schema is not installed system-wide, Traygolin compiles a copy into the
user cache and sets `GSETTINGS_SCHEMA_DIR`.

## Login

Choose Pangolin Cloud or enter your self-hosted server. Traygolin runs
`pangolin login`, opens the device login page in your browser and shows the
code to enter.

## Environment

- `PANGOLIN_BINARY`: path to the CLI if it is not `pangolin` on `PATH`. The
  privileged helper still only runs `/usr/bin/pangolin` or
  `/usr/local/bin/pangolin`.
