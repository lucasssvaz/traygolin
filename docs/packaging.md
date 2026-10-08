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
  the `linux-amd64` and `linux-arm64` tarballs from the GitHub release. AUR
  rules require the `-bin` suffix for prebuilt packages, so there is no plain
  `traygolin`.
- [`packaging/aur/traygolin-git/`](../packaging/aur/traygolin-git/): builds the
  latest commit with Arch's recommended Go flags (PIE, `-trimpath`, external
  linking).

Both provide and conflict with `traygolin`, install licenses under their own
package name, and are `license=('Apache-2.0' 'MIT')` because of the
Trayscale-derived files.

The AUR git repositories must contain only `PKGBUILD` and `.SRCINFO`.
Regenerate `.SRCINFO` with `makepkg --printsrcinfo` after editing a PKGBUILD.

## Debian package

`packaging/debian/build-deb.sh` (or `make deb`) stages `PREFIX=/usr` and wraps
it with `dpkg-deb`. Run it on Ubuntu 26.04+ so the binaries link that distro's
GTK. Maintainer scripts compile GSettings schemas and refresh the icon and
desktop databases.

## GitHub Releases

Tagging `vX.Y.Z` builds four artifacts (fail-fast is off, so one arch can fail
without blocking the others):

| File | Built on |
| --- | --- |
| `traygolin-X.Y.Z-linux-amd64.tar.gz` | `archlinux:latest` |
| `traygolin-X.Y.Z-linux-arm64.tar.gz` | `ghcr.io/fwcd/archlinux` on `ubuntu-24.04-arm` (Arch Linux ARM; the official Arch image is amd64-only) |
| `traygolin_X.Y.Z-1_amd64.deb` | `ubuntu:26.04` |
| `traygolin_X.Y.Z-1_arm64.deb` | `ubuntu:26.04` on `ubuntu-24.04-arm` |

`traygolin-bin` installs the Arch tarball for the machine's CARCH.

## Apt repository (GitHub Pages)

`.github/workflows/pages.yml` runs after a release (and from **workflow_dispatch**).
It downloads every `*.deb` from GitHub Releases, builds `dists/stable` with
`dpkg-scanpackages`, and deploys to GitHub Pages. Enable Pages with source
**GitHub Actions**. Optional secret `APT_GPG_PRIVATE_KEY` signs `InRelease`.

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
