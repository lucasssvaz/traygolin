# Install

## Pangolin CLI

Traygolin does not bundle Pangolin. Install the official CLI first:

```bash
curl -fsSL https://static.pangolin.net/get-cli.sh | bash
pangolin version
```

The privileged helper only runs a `pangolin` binary that is owned by root and
not writable by anyone else, in `/usr/bin` or `/usr/local/bin`. The install
script and the AUR `pangolin-cli` package both put it there.

## Build dependencies

- Go >= 1.24
- Libadwaita >= 1.9 and the GTK 4 version it requires
- gobject-introspection, pkg-config, glib-compile-schemas
- polkit at runtime

On Arch/CachyOS:

```bash
sudo pacman -S go gtk4 libadwaita gobject-introspection polkit
```

## Build and install

```bash
make
sudo make install            # PREFIX=/usr by default
```

This installs:

| Path | Purpose |
| --- | --- |
| `/usr/bin/traygolin` | the app |
| `/usr/lib/traygolin/traygolin-helper` | root helper started through polkit |
| `/usr/share/polkit-1/actions/io.github.lucasssvaz.Traygolin.policy` | lets the active user run the helper without a password |
| desktop file, metainfo, icons, GSettings schema, `traygolin(1)` | desktop integration |

Uninstall with `sudo make uninstall`.

With a different `PREFIX`, the helper path is compiled into both binaries and
the policy, so they stay consistent. The policy always goes to
`/usr/share/polkit-1/actions`, the only place polkit reads.

## Running a development build

If you run `./bin/traygolin` without installing, Traygolin shows **Setup
Required** when you try to connect. **Set Up…** copies `bin/traygolin-helper`
and the policy into place with one administrator prompt. After that, Connect
needs no password.

## Removing the old setcap grant

Earlier Traygolin versions asked you to run `setcap` on the CLI. That never
removed the sudo prompt and is no longer used. To remove it:

```bash
sudo setcap -r "$(command -v pangolin)"
```

## Autostart

Turn on **Start at Login** in Preferences → General. Traygolin writes
`~/.config/autostart/io.github.lucasssvaz.Traygolin.desktop` with `--hide-window`,
so it starts in the tray only. The entry is refreshed each time Traygolin
starts, so it follows the binary if you switch from a source build to a
package, and its `TryExec` makes the desktop skip it once Traygolin is
uninstalled.

Turn on **Connect at Start** to connect whenever Traygolin starts, if you are
logged in and setup is done. Combined with Start at Login, this connects
right after you sign in, without a password prompt.

## Troubleshooting

**Connect still asks for a password.** The polkit rule only grants
passwordless access to the active local session. Over SSH or from a
locked/inactive session, polkit asks for an administrator password instead.
Check that the policy exists:

```bash
pkaction --action-id io.github.lucasssvaz.Traygolin.tunnel --verbose
```

**Setup Required keeps coming back.** The helper only runs a root-owned CLI.
The official install script can leave `/usr/local/bin/pangolin` owned by your
user; **Set Up…** fixes that, or run
`sudo chown root:root /usr/local/bin/pangolin`. A CLI installed somewhere else,
such as `~/.local/bin`, must be moved to `/usr/local/bin` first.

**Aliases stop resolving after a crash.** Preferences → DNS → Reset DNS.

**Something else.** Main menu → View Logs shows the Pangolin client log.
