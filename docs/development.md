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

GitHub Actions run in an `archlinux:latest` container because the UI needs
Libadwaita 1.9. Jobs: `go test -race` under xvfb, golangci-lint, `make` plus a
staged `make install`, and AppStream metainfo validation.

## License headers

New files use the Apache-2.0 header. Files that copy or adapt Trayscale code
also add the Trayscale line; see [CONTRIBUTING.md](../CONTRIBUTING.md).
