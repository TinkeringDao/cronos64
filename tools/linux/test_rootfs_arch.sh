#!/bin/sh
# Hermetic test for the root filesystem architecture (--arch) of mkrootfs.sh and deploy-rootfs.sh: no
# build, no network, no unit. deploy-rootfs.sh --print-arch says what it would build and stops.
HERE=$(cd "$(dirname "$0")" && pwd)
T=$(mktemp -d) || exit 1
trap 'rm -rf "$T"' EXIT
fail=0
check() { # name actual expected
	if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2', want '$3'"; fail=1; fi
}

# --- deploy-rootfs.sh: which architecture, which Go target, which daemon file
plan() { env -u DEVICE_OVERLAY "$@" bash "$HERE/deploy-rootfs.sh" --print-arch 2>&1; }
check "default (the Show image) is armv7" "$(plan)" "arch=armv7 goarch=arm daemon=bin/echod-arm"
mkdir -p "$T/cronos/etc/techo5" "$T/athena/etc/techo5"
echo T5_BOARD=cronos > "$T/cronos/etc/techo5/device.conf"
echo T5_BOARD=athena > "$T/athena/etc/techo5/device.conf"
check "a cronos overlay defaults to aarch64" "$(plan DEVICE_OVERLAY="$T/cronos")" \
	"arch=aarch64 goarch=arm64 daemon=bin/echod-arm64"
check "another board's overlay stays armv7" "$(plan DEVICE_OVERLAY="$T/athena")" \
	"arch=armv7 goarch=arm daemon=bin/echod-arm"
check "--arch aarch64 on the Show image" \
	"$(env -u DEVICE_OVERLAY bash "$HERE/deploy-rootfs.sh" --arch aarch64 --print-arch 2>&1)" \
	"arch=aarch64 goarch=arm64 daemon=bin/echod-arm64"
check "--arch armv7 overrides a cronos overlay" \
	"$(DEVICE_OVERLAY="$T/cronos" bash "$HERE/deploy-rootfs.sh" --arch armv7 --print-arch 2>&1)" \
	"arch=armv7 goarch=arm daemon=bin/echod-arm"
out=$(env -u DEVICE_OVERLAY bash "$HERE/deploy-rootfs.sh" --arch x86_64 --print-arch 2>&1); rc=$?
check "--arch x86_64 is refused" "$rc:$out" "1:--arch takes armv7 or aarch64, not x86_64"

# --- mkrootfs.sh: --arch picks the base (and is apk's --arch), and only the two known values
IN=$T/in
mkdir -p "$IN/inputs" "$IN/tools"
: > "$IN/inputs/alpine-minirootfs-3.24.2-armv7.tar.gz"
out=$(sh "$HERE/mkrootfs.sh" -i "$IN" -o "$T/out.tgz" -w "$T/w" -W alpine --arch aarch64 2>&1); rc=$?
check "mkrootfs --arch aarch64 wants the aarch64 base" "$rc:$out" \
	"1:mkrootfs: no inputs/alpine-minirootfs-*-aarch64.tar.gz"
out=$(sh "$HERE/mkrootfs.sh" -i "$IN" -o "$T/out.tgz" -w "$T/w" -W alpine --arch riscv64 2>&1); rc=$?
check "mkrootfs --arch riscv64 is refused" "$rc:$out" "1:mkrootfs: --arch takes armv7 or aarch64, not riscv64"
out=$(sh "$HERE/mkrootfs.sh" -i "$IN" -o "$T/out.tgz" -w "$T/w" -W alpine -a riscv64 2>&1); rc=$?
check "mkrootfs -a riscv64 is refused too" "$rc:$out" "1:mkrootfs: --arch takes armv7 or aarch64, not riscv64"

# --- wsl-build.sh hands its fourth argument to mkrootfs.sh as -a (armv7 when absent)
grep -q -- '-a "$ARCH"' "$HERE/wsl-build.sh"; check "wsl-build.sh passes the architecture on" "$?" 0

exit $fail
