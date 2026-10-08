#!/bin/bash
# Build a Debian apt repo in OUT from every .deb in DEBDIR.
# Optional: ASCII-armored private key in APT_GPG_PRIVATE_KEY to sign Release.
set -euo pipefail

debdir=${1:?usage: generate-repo.sh DEBDIR OUT [BASEURL]}
out=${2:?}
baseurl=${3:-}

root=$(cd "$(dirname "$0")/../.." && pwd)
rm -rf "$out"
mkdir -p "$out/pool/main/t/traygolin"

shopt -s nullglob
debs=("$debdir"/*.deb)
if [ ${#debs[@]} -eq 0 ]; then
	echo "generate-repo.sh: no .deb files in $debdir" >&2
	exit 1
fi
cp -a "${debs[@]}" "$out/pool/main/t/traygolin/"

arches=$(
	for f in "$out"/pool/main/t/traygolin/*.deb; do
		dpkg-deb -f "$f" Architecture
	done | sort -u | tr '\n' ' '
)
arches=${arches%% }

for arch in $arches; do
	bindir="$out/dists/stable/main/binary-${arch}"
	mkdir -p "$bindir"
	(
		cd "$out"
		dpkg-scanpackages --arch "$arch" pool /dev/null
	) >"$bindir/Packages"
	gzip -9n -c "$bindir/Packages" >"$bindir/Packages.gz"
done

conf=$(mktemp)
GNUPGHOME=""
cleanup() {
	rm -f "$conf"
	if [ -n "${GNUPGHOME:-}" ]; then
		rm -rf "$GNUPGHOME"
	fi
}
trap cleanup EXIT
cat >"$conf" <<EOF
APT::FTPArchive::Release {
	Origin "Traygolin";
	Label "Traygolin";
	Suite "stable";
	Codename "stable";
	Architectures "$arches";
	Components "main";
	Description "Unofficial apt repository for Traygolin";
};
EOF
apt-ftparchive -c "$conf" release "$out/dists/stable" >"$out/dists/stable/Release"

signed=0
if [ -n "${APT_GPG_PRIVATE_KEY:-}" ]; then
	export GNUPGHOME
	GNUPGHOME=$(mktemp -d)
	printf '%s\n' "$APT_GPG_PRIVATE_KEY" | gpg --batch --import
	gpg --batch --yes --armor --export >"$out/traygolin.asc"
	gpg --batch --yes --export >"$out/traygolin.gpg"
	gpg --batch --yes --clearsign -o "$out/dists/stable/InRelease" "$out/dists/stable/Release"
	gpg --batch --yes -abs -o "$out/dists/stable/Release.gpg" "$out/dists/stable/Release"
	signed=1
fi

if [ -z "$baseurl" ]; then
	baseurl="https://example.invalid/traygolin"
fi
baseurl=${baseurl%/}

if [ "$signed" = 1 ]; then
	setup=$(cat <<EOF
<pre>curl -fsSL ${baseurl}/traygolin.asc | sudo tee /etc/apt/keyrings/traygolin.asc >/dev/null
echo 'deb [signed-by=/etc/apt/keyrings/traygolin.asc] ${baseurl} stable main' | sudo tee /etc/apt/sources.list.d/traygolin.list
sudo apt update
sudo apt install traygolin</pre>
EOF
)
else
	setup=$(cat <<EOF
<pre>echo 'deb [trusted=yes] ${baseurl} stable main' | sudo tee /etc/apt/sources.list.d/traygolin.list
sudo apt update
sudo apt install traygolin</pre>
<p>This repository is not signed. Add an <code>APT_GPG_PRIVATE_KEY</code>
repository secret to publish a signed InRelease.</p>
EOF
)
fi

python3 - "$root/packaging/apt/index.html" "$out/index.html" "$setup" <<'PY'
import pathlib, sys
src, dest, setup = sys.argv[1], sys.argv[2], sys.argv[3]
text = pathlib.Path(src).read_text()
pathlib.Path(dest).write_text(text.replace("<!-- APT_SETUP -->", setup, 1))
PY
