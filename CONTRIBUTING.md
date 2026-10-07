# Contributing

## License

New files use the Apache License 2.0 header used by existing Go sources.

Code copied or adapted from [Trayscale](https://github.com/DeedleFake/trayscale)
is allowed under its MIT license, but in the same change you must:

- add this line under the copyright line of the file:
  `Portions Copyright (c) 2025 DeedleFake, MIT License; see LICENSES/MIT-Trayscale.txt`
- list the file in the "Portions derived from Trayscale" section of `NOTICE`.

Do not copy code from the Pangolin CLI (fosrl/cli). It is AGPL-3.0, which is
not compatible with distributing Traygolin under Apache-2.0. Traygolin may
only run the installed binary and speak its local socket protocol.

## Security

`internal/privhelper` runs as root. Any new `pangolin up` flag must be added to
the allow-list in `validate.go` with a value check and a test. Never let the
helper run an arbitrary binary or accept arguments that open listeners or read
files chosen by the caller.

## Development

See [docs/development.md](docs/development.md).

```
make test
make
```
