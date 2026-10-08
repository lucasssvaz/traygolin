#!/bin/bash
# Build a .deb from the staged PREFIX=/usr tree. Run on Ubuntu 26.04+ so
# the binaries link the distro's GTK 4 / Libadwaita.
set -euo pipefail

root=$(cd "$(dirname "$0")/../.." && pwd)
cd "$root"

if ! command -v dpkg-deb >/dev/null; then
	echo "build-deb.sh: dpkg-deb is required (Debian/Ubuntu)" >&2
	exit 1
fi

deb_arch=${DEB_HOST_ARCH:-$(dpkg --print-architecture)}
raw=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)}
raw=${raw#v}
raw=${raw//_/.}
if [[ $raw =~ ^[0-9]+\.[0-9]+ ]]; then
	version=$raw
else
	# Untagged builds: Debian wants a leading digit, and treats the last
	# hyphen as the revision separator.
	version="0.0.0+${raw//-/.}"
fi

dest=$(mktemp -d)
trap 'rm -rf "$dest"' EXIT

make VERSION="$version" DESTDIR="$dest" PREFIX=/usr install
install -Dm644 LICENSE "$dest/usr/share/doc/traygolin/copyright"
install -Dm644 NOTICE "$dest/usr/share/doc/traygolin/NOTICE"

mkdir -p "$dest/DEBIAN"
# Installed-Size is KiB of the payload, not the control files.
size=$(du -sk --exclude=DEBIAN "$dest" | cut -f1)
sed \
	-e "s/@VERSION@/${version}/g" \
	-e "s/@ARCH@/${deb_arch}/g" \
	-e "s/@INSTALLED_SIZE@/${size}/g" \
	packaging/debian/control.in >"$dest/DEBIAN/control"
install -m755 packaging/debian/postinst "$dest/DEBIAN/postinst"
install -m755 packaging/debian/postrm "$dest/DEBIAN/postrm"

out="$root/traygolin_${version}-1_${deb_arch}.deb"
dpkg-deb --root-owner-group --build "$dest" "$out"
echo "$out"
