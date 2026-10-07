# Traygolin

Unofficial Linux tray app for the [Pangolin](https://pangolin.net) VPN client.

There is no official Pangolin desktop app for Linux. Traygolin is a GTK4 and
Libadwaita app that drives the official `pangolin` CLI, so you can connect,
switch accounts and organizations, use an exit node, and change the same DNS
and routing options as the Windows and macOS clients, without a terminal or a
password prompt.

**This project is not affiliated with Fossorial, Pangolin, or Trayscale.**

The window, tray and status polling are modelled on
[Trayscale](https://github.com/DeedleFake/trayscale), and some of its MIT
licensed code is used with attribution. See [NOTICE](NOTICE).

## Requirements

- Linux with GTK 4 and Libadwaita 1.9 or newer (Ubuntu 26.04 LTS or later, or current Arch)
- polkit (for passwordless Connect)
- The official Pangolin CLI installed in `/usr/bin` or `/usr/local/bin`
- Go 1.24+ to build from source

Install the CLI:

```bash
curl -fsSL https://static.pangolin.net/get-cli.sh | bash
```

## Install

### AUR (Arch Linux)

```bash
paru -S traygolin-bin    # prebuilt release (x86_64)
paru -S traygolin-git    # built from the latest commit
```

### Ubuntu 26.04 or later

```bash
sudo apt install golang-go gcc pkg-config make git \
  libgtk-4-dev libadwaita-1-dev libgirepository1.0-dev \
  gobject-introspection gir1.2-gtk-4.0 gir1.2-adw-1 \
  pkexec polkitd
```

Then follow **From source** below. Ubuntu 24.04 LTS cannot be used: it ships
Libadwaita 1.5 / GTK 4.14, and Libadwaita 1.9 needs GTK 4.21 and GLib 2.84.
See [docs/install.md](docs/install.md).

### From source

```bash
make
sudo make install
```

`make install` also installs a small privileged helper in
`/usr/lib/traygolin/` and a polkit rule, which is what lets Connect work
without a password. See [docs/install.md](docs/install.md).

## Usage

Start **Traygolin** from your app menu. The switch in the window's header bar
connects and disconnects, like Trayscale. The sidebar lists **This Device** and
each Pangolin site with its connection path (Direct, Relay or Local). The tray
icon shows whether you are connected or using an exit node, and its menu has
Connect, your accounts, organization and exit node.

Closing the window keeps Traygolin in the tray; choose **Quit** to exit.
`--hide-window` starts in the tray only.

More: [docs/usage.md](docs/usage.md).

## How Connect works without a password

The Pangolin CLI starts its tunnel by calling `sudo`. When Traygolin runs the
CLI, that `sudo` call reaches Traygolin instead. Traygolin checks the exact
command, then asks polkit to run its helper as root. The polkit rule allows
this for the active local user without a password, the same way
NetworkManager VPNs work. The helper only starts the root-owned `pangolin`
binary with `pangolin up` options it recognises.

Disconnect, status, organization and exit-node changes talk to the running
tunnel directly and need no privileges.

Details: [docs/architecture.md](docs/architecture.md).

## License

Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE). Portions
derived from Trayscale are MIT licensed; see
[LICENSES/MIT-Trayscale.txt](LICENSES/MIT-Trayscale.txt).

The Pangolin name and logo are Fossorial trademarks and are not used in
Traygolin's icon.
