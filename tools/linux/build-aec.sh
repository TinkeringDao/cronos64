#!/bin/bash
# build-aec.sh — compile tools/aec/techo5-aec.cpp for armv7 or aarch64 inside an Alpine root
# (QEMU user emulation under WSL/x86_64, or native on aarch64 hosts). The first run makes the root
# (~/alpine-<arch>-sdk) from the minirootfs and installs the compiler and webrtc-audio-processing; later runs reuse it.
#
#   tools/linux/build-aec.sh [--arch armv7|aarch64] [-o bin/techo5-aec-arm]
#
# Needs what wsl-build.sh needs (qemu-user-static, binfmt-support, uidmap, ~/apk/apk.static)
# and the Alpine minirootfs from TECHO5_INPUTS (default: inputs/ in this repository).
set -euo pipefail
# The repo: from this script's place, or TECHO5_ROOT when the script was copied elsewhere (deploy
# copies it to a Linux path so WSL's bash does not trip over CRLF).
ROOT=${TECHO5_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}
export TECHO5_ROOT=$ROOT
INPUTS=${TECHO5_INPUTS:-$ROOT/inputs}
APK=$HOME/apk/apk.static
ARCH=armv7
OUT=
while [ $# -gt 0 ]; do
	case "$1" in
	-a|--arch) ARCH=$2; shift 2;;
	-o) OUT=$2; shift 2;;
	*) echo "unknown argument: $1" >&2; exit 1;;
	esac
done

case "$ARCH" in
armv7) SFX=arm;;
aarch64) SFX=arm64;;
*) echo "build-aec.sh: --arch takes armv7 or aarch64, not $ARCH" >&2; exit 1;;
esac
[ -n "$OUT" ] || OUT=$ROOT/bin/techo5-aec-$SFX
SDK=$HOME/alpine-$ARCH-sdk

if [ "$(id -u)" -ne 0 ] && [ -z "${TECHO5_IN_NS:-}" ]; then
	exec unshare -Ur --map-auto env TECHO5_IN_NS=1 bash "$0" --arch "$ARCH" -o "$OUT"
fi

if [ ! -x "$SDK/usr/bin/g++" ]; then
	echo "== making the $ARCH SDK root at $SDK"
	rm -rf "$SDK"; mkdir -p "$SDK"
	tar -xzf "$(ls "$INPUTS"/alpine-minirootfs-*-"$ARCH".tar.gz | head -1)" -C "$SDK"
	cp /etc/resolv.conf "$SDK/etc/resolv.conf"
	"$APK" --root "$SDK" --arch "$ARCH" --no-cache add build-base webrtc-audio-processing-1-dev pkgconf
fi

mkdir -p "$SDK/build"
cp "$ROOT/tools/aec/techo5-aec.cpp" "$SDK/build/"
echo "== compiling for $ARCH"
chroot "$SDK" /bin/sh -c 'cd /build && g++ -O2 -std=c++17 -o techo5-aec techo5-aec.cpp $(pkg-config --cflags --libs webrtc-audio-processing-1) && strip techo5-aec && ls -la techo5-aec'
mkdir -p "$(dirname "$OUT")"
cp "$SDK/build/techo5-aec" "$OUT"
echo "built: $OUT"
