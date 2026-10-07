# Development

## Layout

- `cmd/traygolin`: app entry; also handles being started as `sudo` by the CLI
- `cmd/traygolin-helper`: root helper started through polkit
- `internal/olm`: tunnel socket client
- `internal/pangolin`: CLI wrapper and config/account readers
- `internal/privhelper`: sudo shim, helper and polkit setup
- `internal/poller`: status poller
- `internal/tray`: StatusNotifierItem menu (`deedles.dev/tray`)
- `internal/ui`: Libadwaita window, pages, preferences and dialogs (`.ui` files are embedded)
- `internal/gutil`: GtkBuilder helpers
- `internal/autostart`: XDG autostart desktop file
- `internal/schema`: compile embedded GSettings XML

## Commands

```bash
make test
make
./bin/traygolin --hide-window --poll-interval 2
```

To try passwordless Connect from a development build, click **Set Up…** once.
It installs `bin/traygolin-helper` and the polkit policy.

## Tests

Most packages need no display. `internal/olm` and `internal/poller` use a fake
olm server on a temporary Unix socket, and `internal/pangolin` uses a fake
`pangolin` shell script. `internal/ui` tests build the real window, pages and
preferences dialog. They skip without `DISPLAY`/`WAYLAND_DISPLAY`:

```bash
DISPLAY=:0 go test ./internal/ui/
xvfb-run -a go test ./...
```

The first gotk4 compile is slow (cgo against GTK). Later builds use the Go cache.

## CI

GitHub Actions host runners may still be Ubuntu 24.04 (Libadwaita 1.5), so the
test job runs in containers: `archlinux:latest` and `ubuntu:26.04`. Each distro
has its own Go build cache. Jobs: `make`, tests (GTK packages under xvfb,
everything else with `-race`), a staged `make install`, golangci-lint (Arch),
and AppStream metainfo validation.

A cold gotk4 compile takes most of a CI job, so the Go build and module caches
are saved between runs, keyed on the distro, Go version, and `go.sum`. The first
run after either changes is slow; later runs reuse the cache. Packages that
import gotk4 are tested without `-race` so gotk4 is only compiled once per job
(it also fails `-race`'s checkptr checks).

## License headers

New files use the Apache-2.0 header. Files that copy or adapt Trayscale code
also add the Trayscale line; see [CONTRIBUTING.md](../CONTRIBUTING.md).
