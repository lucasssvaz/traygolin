#!/bin/bash
# Verify a PREFIX=/usr install of Traygolin: files, --help, helper, and a
# short GTK run under xvfb. Used after apt/AUR install in Release.
set -euo pipefail

APPID=io.github.lucasssvaz.Traygolin
BIN=/usr/bin/traygolin
HELPER=/usr/lib/traygolin/traygolin-helper

die() {
	echo "smoke-installed: $*" >&2
	exit 1
}

test -x "$BIN" || die "missing $BIN"
test -x "$HELPER" || die "missing $HELPER"
command -v traygolin >/dev/null || die "traygolin is not on PATH"
test ! -w "$HELPER" || [ "$(id -u)" -eq 0 ] || die "$HELPER is writable"
mode=$(stat -c '%a' "$HELPER")
[ "$mode" = 755 ] || die "$HELPER mode is $mode, expected 755"
test -f /usr/share/glib-2.0/schemas/gschemas.compiled ||
	die "gschemas.compiled missing (package hooks did not compile schemas)"
test -f "/usr/share/applications/${APPID}.desktop" || die "missing desktop file"
test -f "/usr/share/metainfo/${APPID}.metainfo.xml" || die "missing metainfo"
test -f "/usr/share/glib-2.0/schemas/${APPID}.gschema.xml" || die "missing gschema"
test -f "/usr/share/polkit-1/actions/${APPID}.policy" || die "missing polkit policy"
test -f "/usr/share/icons/hicolor/256x256/apps/${APPID}.png" || die "missing png icon"
test -f "/usr/share/icons/hicolor/scalable/apps/${APPID}.svg" || die "missing svg icon"
if [ -f /usr/share/man/man1/traygolin.1 ] || [ -f /usr/share/man/man1/traygolin.1.gz ]; then
	:
elif grep -qrE 'path-exclude=.*/usr/share/man' /etc/dpkg 2>/dev/null ||
	grep -qE '^[[:space:]]*NoExtract.*usr/share/man' /etc/pacman.conf 2>/dev/null; then
	echo "smoke-installed: man page not extracted (image excludes /usr/share/man)"
else
	die "missing man page"
fi

if grep -F "$HELPER" "/usr/share/polkit-1/actions/${APPID}.policy" >/dev/null; then
	:
else
	die "polkit policy does not pin $HELPER"
fi

notice=
mit=
for dir in /usr/share/licenses/traygolin /usr/share/licenses/traygolin-bin \
	/usr/share/licenses/traygolin-git /usr/share/doc/traygolin; do
	if [ -z "$notice" ] && [ -f "$dir/NOTICE" ]; then
		notice=$dir/NOTICE
	fi
	if [ -z "$mit" ] && [ -f "$dir/MIT-Trayscale.txt" ]; then
		mit=$dir/MIT-Trayscale.txt
	fi
done
[ -n "$notice" ] || die "missing NOTICE under /usr/share/licenses or /usr/share/doc/traygolin"
[ -n "$mit" ] || die "missing MIT-Trayscale.txt under /usr/share/licenses or /usr/share/doc/traygolin"

if [ -z "${GSK_RENDERER:-}" ]; then
	export GSK_RENDERER=cairo
fi
if [ -z "${GDK_BACKEND:-}" ]; then
	export GDK_BACKEND=x11
fi

run_xvfb() {
	if command -v dbus-run-session >/dev/null; then
		xvfb-run -a dbus-run-session -- "$@"
	else
		xvfb-run -a "$@"
	fi
}

help=$(run_xvfb "$BIN" --help)
case $help in
	*"--hide-window"*) ;;
	*) die "--help does not mention --hide-window" ;;
esac
case $help in
	*"--poll-interval"*) ;;
	*) die "--help does not mention --poll-interval" ;;
esac

set +e
"$HELPER" 2>/tmp/traygolin-helper.err
helper_rc=$?
set -e
[ "$helper_rc" -ne 0 ] || die "traygolin-helper should refuse to run without pkexec"
grep -F 'PKEXEC_UID is not set' /tmp/traygolin-helper.err >/dev/null ||
	die "helper stderr missing PKEXEC_UID is not set"

set +e
PKEXEC_UID=0 "$HELPER" 2>/tmp/traygolin-helper-usage.err
usage_rc=$?
set -e
[ "$usage_rc" -ne 0 ] || die "traygolin-helper should fail without a command"
grep -F 'usage: traygolin-helper' /tmp/traygolin-helper-usage.err >/dev/null ||
	die "helper stderr missing usage line"

if command -v desktop-file-validate >/dev/null; then
	desktop-file-validate "/usr/share/applications/${APPID}.desktop"
fi

# Stay up in the tray, then SIGTERM. 0 = handled TERM; 124 = still running
# when timeout fired; 137 = SIGKILL after --kill-after.
set +e
if command -v dbus-run-session >/dev/null; then
	timeout --signal=TERM --kill-after=4s 6 xvfb-run -a dbus-run-session -- \
		"$BIN" --hide-window --poll-interval 1
else
	timeout --signal=TERM --kill-after=4s 6 xvfb-run -a \
		"$BIN" --hide-window --poll-interval 1
fi
gui_rc=$?
set -e
case $gui_rc in
	0 | 124 | 137) ;;
	*) die "traygolin --hide-window exited $gui_rc (expected stay-up then TERM)" ;;
esac

echo "smoke-installed: ok ($(uname -m)) $BIN"
