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

# --- deploy-rootfs.sh: --stage-only and helper64 dynamic aarch64 support
out=$(bash "$HERE/deploy-rootfs.sh" --stage-only --arch x86_64 2>&1); rc=$?
check "deploy-rootfs.sh --stage-only parses alongside --arch validation" "$rc:$out" "1:--arch takes armv7 or aarch64, not x86_64"

python3 - <<'PY' "$HERE/deploy-rootfs.sh" "$T/bin" "$T/stage" || fail=1
import os, pathlib, subprocess, sys

script, bin_dir, stage_dir = sys.argv[1:]
os.makedirs(bin_dir, exist_ok=True)
os.makedirs(os.path.join(stage_dir, "bin"), exist_ok=True)

def write_elf(name, elf_class, e_machine, tail=b""):
    hdr = bytearray(64)
    hdr[0:4] = b"\x7fELF"
    hdr[4] = elf_class
    hdr[18:20] = e_machine.to_bytes(2, "little")
    p = pathlib.Path(bin_dir) / name
    p.write_bytes(bytes(hdr) + tail)

# 1. Dynamic musl ELF64 aarch64 helper (techo5-aec) must be accepted
write_elf("techo5-aec-arm64", 2, 183, b"/lib/ld-musl-aarch64.so.1\x00")
# 2. Static ELF32 arm helper (techo5-librespot) must still be accepted
write_elf("techo5-librespot-arm", 1, 40, b"")
# 3. Dynamic ELF32 arm helper (techo5-ffmpeg) must still be skipped
write_elf("techo5-ffmpeg-arm", 1, 40, b"/lib/ld-musl-armhf.so.1\x00")
# 4. glibc-linked ELF64 aarch64 helper must be rejected
write_elf("techo5-nqptp-arm64", 2, 183, b"/lib/ld-linux-aarch64.so.1\x00")

# Extract elf_is, is_static, and helper64 cleanly from deploy-rootfs.sh without quoting hazards.
lines = pathlib.Path(script).read_text().splitlines()
s1 = next(i for i, l in enumerate(lines) if l.startswith("elf_is()"))
e1 = next(i for i, l in enumerate(lines) if l.startswith("case \"$ARCH\" in armv7)"))
s2 = next(i for i, l in enumerate(lines) if l.startswith("helper64()"))
e2 = next(i for i, l in enumerate(lines) if l.startswith("if [ \"$ARCH\" = aarch64 ]"))
snippet = "\n".join(lines[s1:e1] + lines[s2:e2])
body = f"""set -euo pipefail
ROOT={os.path.dirname(bin_dir)!r}
STAGE={stage_dir!r}
{snippet}
for b in techo5-aec techo5-librespot techo5-ffmpeg techo5-nqptp; do
    helper64 "$b"
done
"""
res = subprocess.run(["bash", "-s"], input=body, capture_output=True, text=True)
assert res.returncode == 0, res.stderr
assert os.path.exists(os.path.join(stage_dir, "bin", "techo5-aec")), "dynamic musl arm64 helper was skipped"
assert os.path.exists(os.path.join(stage_dir, "bin", "techo5-librespot")), "static armv7 helper was skipped"
assert not os.path.exists(os.path.join(stage_dir, "bin", "techo5-ffmpeg")), "dynamic armv7 helper was copied"
assert not os.path.exists(os.path.join(stage_dir, "bin", "techo5-nqptp")), "glibc arm64 helper was copied"
PY
check "helper64 accepts musl aarch64 and static armv7 only" "$?" 0

# --- build-aec.sh: --arch aarch64 usage validation
out=$(bash "$HERE/build-aec.sh" --arch riscv64 2>&1); rc=$?
check "build-aec.sh --arch riscv64 is refused" "$rc:$out" "1:build-aec.sh: --arch takes armv7 or aarch64, not riscv64"

exit $fail
