# Packaging

## Source install

`make install` (`PREFIX=/usr` by default) places:

- `/usr/bin/traygolin`
- `/usr/lib/traygolin/traygolin-helper` (0755, root)
- `/usr/share/polkit-1/actions/io.github.lucasssvaz.Traygolin.policy`, rendered from
  `io.github.lucasssvaz.Traygolin.policy.in` with the helper path
- desktop file, metainfo, 256×256 and scalable icons
- GSettings schema, compiled only when `DESTDIR` is empty so packages do not
  ship `gschemas.compiled`
- `traygolin(1)`
- `/usr/share/licenses/traygolin/{NOTICE,MIT-Trayscale.txt}`, overridable with
  `LICENSEDIR`

The helper path is compiled into both binaries with
`-X …/internal/privhelper.HelperPath`, and the polkit policy pins the same
path. Packagers who change `LIBDIR` get a consistent result automatically.
Extra Go linker flags go in `GO_LDFLAGS` (for example `-linkmode=external`),
since `-ldflags` in `GOFLAGS` is overridden by the Makefile's own.

Runtime dependencies: `gtk4`, `libadwaita>=1.9`, `polkit`, and the Pangolin CLI.
On Ubuntu 26.04+ those packages are `libgtk-4-1`, `libadwaita-1-0`, `pkexec`,
and `polkitd`.

## AUR

Templates:

- [`packaging/aur/traygolin-bin/`](../packaging/aur/traygolin-bin/): repackages
  the x86_64 tarball from the GitHub release. AUR rules require the `-bin`
  suffix for prebuilt packages, so there is no plain `traygolin`.
- [`packaging/aur/traygolin-git/`](../packaging/aur/traygolin-git/): builds the
  latest commit with Arch's recommended Go flags (PIE, `-trimpath`, external
  linking).

Both provide and conflict with `traygolin`, install licenses under their own
package name, and are `license=('Apache-2.0' 'MIT')` because of the
Trayscale-derived files.

The AUR git repositories must contain only `PKGBUILD` and `.SRCINFO`.
Regenerate `.SRCINFO` with `makepkg --printsrcinfo` after editing a PKGBUILD.

## GitHub Releases

Tagging `vX.Y.Z` builds in an Arch Linux container and publishes
`traygolin-X.Y.Z-linux-amd64.tar.gz`, the staged `make install PREFIX=/usr`
tree, with a `SHA256SUMS` file. `traygolin-bin` installs that tarball as is.

## AUR publishing

After the release job, the Release workflow calls `.github/workflows/aur.yml`.
It sets `traygolin-bin` to the new version and checksum, syncs the
`traygolin-git` PKGBUILD, regenerates `.SRCINFO` with `makepkg`, and pushes
only when something changed. The checksum comes from the release job (or the
release's `SHA256SUMS` when run by hand), and publishing fails if the
downloaded tarball does not match it. Run it by hand from the Actions tab to republish;
leave the version empty to sync only `traygolin-git`.

It needs an `AUR_SSH_PRIVATE_KEY` repository secret whose public key is on your
AUR account. Without the secret it skips publishing and succeeds. The first
push creates each AUR package.
