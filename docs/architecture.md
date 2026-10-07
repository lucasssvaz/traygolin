# Architecture

Traygolin follows Trayscale's structure: a poller sends status updates to the
GTK main loop, which updates the tray and a split-view window. Pangolin on
Linux has no daemon API like `tailscaled`, so Traygolin combines four
sources:

```
                 ┌──────────────── internal/poller ────────────────┐
                 │  tunnel     accounts/config     server      CLI  │
                 └────┬────────────┬─────────────────┬─────────┬──┘
                      │            │                 │         │
   /var/run/olm.sock ─┘  ~/.config/pangolin/*.json   │         └─ pangolin version
   (internal/olm)        (internal/pangolin)         │
                                    https://<server>/api/v1 (orgs, exit nodes)
                                    (internal/server, once a minute)

  Connect:     pangolin up --silent ──sudo──▶ traygolin (shim)
                                                │ pkexec
                                                ▼
                                       traygolin-helper (root)
                                                │
                                                ▼
                                    /usr/local/bin/pangolin up … (detached)

  Disconnect:  POST /exit on olm.sock, then wait for the socket to go away
```

## Packages

| Package | Role |
| --- | --- |
| `internal/olm` | Client for the tunnel's local HTTP-over-Unix-socket API: status, exit, switch org, exit node. Independent implementation; no fosrl/cli code. |
| `internal/server` | Read-only client for the Pangolin server API (organizations, exit nodes), authenticated with the CLI's saved session. Independent implementation; no fosrl/cli code. |
| `internal/pangolin` | Runs user-level CLI commands (`up`, `login`, `select`, `config set`, …) and reads `accounts.json` / `config.json` without spawning the CLI. |
| `internal/privhelper` | The `sudo` shim, the root helper and polkit setup. |
| `internal/poller` | Trayscale-style channel poller with fast polling during transitions. |
| `internal/tray` | StatusNotifierItem with Trayscale-style change tracking. |
| `internal/ui` | Libadwaita app, window, pages and dialogs. |

## Privileges

The CLI's `up` command re-runs itself with `sudo sh -c '… nohup pangolin up
client --id … --secret … &'` unless it is already root. Two earlier approaches
failed. `setcap` on the binary cannot help because the CLI checks for UID 0.
Running the whole CLI through `pkexec` loses `SUDO_USER`, so it reads root's
accounts instead of yours.

Traygolin instead puts a directory first on the CLI's `PATH` that contains a
`sudo` symlink to the Traygolin binary. When started as `sudo`, Traygolin:

1. parses only the exact shell command the CLI generates;
2. sends the arguments to `pkexec /usr/lib/traygolin/traygolin-helper up` on
   stdin, so the secret never appears in a process list.

The helper (root, via polkit action `io.github.lucasssvaz.Traygolin.tunnel`):

1. requires `PKEXEC_UID` and resolves that user;
2. validates every flag against an allow-list (no `--attach`, `--http-addr`,
   `--tls-client-cert`, positional args, etc.);
3. runs only a root-owned, non-writable `pangolin` in `/usr/bin` or
   `/usr/local/bin`;
4. sets `SUDO_USER`/`SUDO_UID`/`SUDO_GID` to the caller, so the CLI uses the
   caller's config, exactly as with real `sudo`;
5. starts it detached in its own session.

The polkit action uses `allow_active=yes`, so the active local user connects
without a password. Inactive and remote sessions need an administrator.

Status and Disconnect use the tunnel's socket, which is readable by users. If
it ever is not, Disconnect falls back to `pkexec traygolin-helper down`.

## License boundary

Code copied or adapted from Trayscale keeps its MIT notice. Each such file has
a header pointing to `LICENSES/MIT-Trayscale.txt`, and `NOTICE` lists them.
Nothing is copied from fosrl/cli (AGPL-3.0); Traygolin only runs the installed
binary and speaks its local protocol.
