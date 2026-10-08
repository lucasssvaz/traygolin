# Traygolin

Traygolin is an unofficial Linux tray application for the [Pangolin](https://pangolin.net) VPN client, built with GTK 4 and Libadwaita.

Because Pangolin does not provide an official desktop app for Linux, Traygolin wraps the official `pangolin` CLI to give you a desktop interface, system tray icon, and background status polling. It lets you connect, disconnect, switch accounts and organizations, pick exit nodes, and configure routing and DNS settings without needing a terminal or typing your password every time.

> **Disclaimer:** Traygolin is an independent open-source project and is not affiliated with, endorsed by, or sponsored by Fossorial, Pangolin, or Trayscale.

The interface and polling design are modeled on [Trayscale](https://github.com/DeedleFake/trayscale). Portions adapted from Trayscale are licensed under MIT and attributed in [NOTICE](NOTICE).

---

## Requirements

Before installing Traygolin, ensure you have:

1. **The official Pangolin CLI** installed in `/usr/bin` or `/usr/local/bin`:
   ```bash
   curl -fsSL https://static.pangolin.net/get-cli.sh | bash
   ```
2. **GTK 4 and Libadwaita 1.9 or newer**:
   - Supported: Ubuntu 26.04 LTS (Resolute) or later, Arch Linux / CachyOS, Fedora 42+, or any modern distribution shipping Libadwaita >= 1.9.
   - **Ubuntu 24.04 LTS is not supported** because it only provides Libadwaita 1.5. See [docs/install.md](docs/install.md) for details.
3. **polkit**: Used for passwordless connection elevation. Desktop environments include this by default.

---

## Installation

### Option 1: Pre-built packages

Package managers automatically install all GTK, Libadwaita, and polkit runtime dependencies. You only need the Pangolin CLI installed beforehand. Both `x86_64` (amd64) and `aarch64` (arm64) architectures are supported.

#### Arch Linux (AUR)

Install `traygolin-bin` using an AUR helper such as `paru`:

```bash
paru -S traygolin-bin
```

If you previously ran `sudo make install`, pacman will stop with "conflicting files" for `/usr/lib/traygolin/traygolin-helper` and the polkit policy. Remove the manual install first, from the source tree you installed from:

```bash
sudo make uninstall
```

Or delete the two files by hand:

```bash
sudo rm /usr/lib/traygolin/traygolin-helper \
  /usr/share/polkit-1/actions/io.github.lucasssvaz.Traygolin.policy
```

#### Ubuntu 26.04 or later (Apt repository)

Add the signed repository and install the package:

```bash
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://lucasssvaz.github.io/traygolin/traygolin.asc \
  | sudo tee /etc/apt/keyrings/traygolin.asc >/dev/null

echo "deb [signed-by=/etc/apt/keyrings/traygolin.asc] https://lucasssvaz.github.io/traygolin stable main" \
  | sudo tee /etc/apt/sources.list.d/traygolin.list

sudo apt update
sudo apt install traygolin
```

#### Manual downloads (.deb / tarball)

Pre-built packages are also attached to each [GitHub Release](https://github.com/lucasssvaz/traygolin/releases).

- If installing the `.deb` directly (`sudo apt install ./traygolin_*.deb`), APT resolves the runtime libraries for you.
- If extracting the `.tar.gz` archive directly to `/usr`, make sure the runtime libraries are present:
  - **Arch:** `sudo pacman -S gtk4 libadwaita gobject-introspection polkit hicolor-icon-theme`
  - **Ubuntu 26.04+:** `sudo apt install libgtk-4-1 libadwaita-1-0 libglib2.0-bin libgirepository-1.0-1 pkexec polkitd hicolor-icon-theme`

---

### Option 2: Building from source

Building from source requires Go 1.24+, a C compiler, and development packages for GTK 4, Libadwaita 1.9, and GObject Introspection.

#### Install build dependencies

**Arch Linux / CachyOS:**
```bash
sudo pacman -S go gtk4 libadwaita gobject-introspection polkit git make
```
*(Alternatively, install `traygolin-git` from the AUR to compile automatically from the latest commit: `paru -S traygolin-git`)*

**Ubuntu 26.04+:**
```bash
sudo apt install golang-go gcc pkg-config make git \
  libgtk-4-dev libadwaita-1-dev libgirepository1.0-dev \
  gobject-introspection gir1.2-gtk-4.0 gir1.2-adw-1 \
  pkexec polkitd
```

#### Compile and install

```bash
make
sudo make install
```

`make install` places the binary in `/usr/bin/traygolin`, installs desktop entries, icons, and man pages, and sets up the root helper and polkit policy under `/usr/lib/traygolin/` and `/usr/share/polkit-1/actions/`.

---

## Usage

Launch **Traygolin** from your desktop application launcher or run `traygolin` in a terminal.

- **Main Window:**
  - Header switch toggles the tunnel connection.
  - Sidebar shows the current device status, connected sites, peer endpoints, latency, and route types (Direct, Relay, or Local).
  - Quick dropdowns allow switching active organizations and exit nodes.
- **System Tray:**
  - The tray icon displays connection state (dimmed when disconnected, solid when connected, badged when an exit node is active).
  - Tray menu provides quick access to connect/disconnect, change accounts, choose organizations, select exit nodes, open preferences, or quit.
- **Background startup:**
  - Pass `--hide-window` to start minimized to the system tray on login:
    ```bash
    traygolin --hide-window
    ```

For detailed interface documentation and configuration options, see [docs/usage.md](docs/usage.md).

---

## How Passwordless Connect Works

The Pangolin CLI normally invokes `sudo` when establishing a tunnel. Traygolin intercepts this call and routes the command through a dedicated privileged helper (`traygolin-helper`) using polkit.

A bundled polkit policy allows the locally logged-in user to bring the tunnel up without entering a password, mirroring the behavior of NetworkManager VPN connections. The helper strictly validates command arguments and only executes the root-owned `pangolin` binary. Disconnecting, checking status, switching organizations, and querying exit nodes talk to the tunnel socket directly and require no elevated permissions.

For the full security model and data flow, see [docs/architecture.md](docs/architecture.md).

---

## Documentation

- [docs/install.md](docs/install.md): In-depth installation notes, distribution details, and troubleshooting.
- [docs/usage.md](docs/usage.md): Feature guide covering preferences, DNS settings, and exit node routing.
- [docs/architecture.md](docs/architecture.md): Internal design, polling pipeline, and privilege separation.
- [docs/packaging.md](docs/packaging.md): Packaging scripts, Debian `.deb` builds, and AUR publishing.
- [docs/development.md](docs/development.md): Development workflow, tests, and CI/CD pipelines.

---

## License

This project is licensed under the Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE) for full terms.

Components adapted from Trayscale are licensed under the MIT License. See [LICENSES/MIT-Trayscale.txt](LICENSES/MIT-Trayscale.txt).

The Pangolin name, trademarks, and associated brand assets belong to Fossorial and are not used in Traygolin's branding or app icons.

---

## AI Disclaimer

Parts of this project, including source code, tests, documentation, packaging scripts, and CI workflows, were developed with the assistance of AI coding tools. All code and configurations have been reviewed, adapted, and tested by human maintainers. If you encounter any bugs, oversights, or unexpected behavior, please report them via [GitHub Issues](https://github.com/lucasssvaz/traygolin/issues).
