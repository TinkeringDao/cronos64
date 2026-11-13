#!/bin/sh
# Unthrottle the LineageOS cronos (Echo Show 5, 1st gen) boot image without rebuilding the kernel.
#
#   unthrottle-cronos-boot.sh <boot_in.img> <boot_out.img>
#
# Edits all eleven appended device trees (patch-dtb.py rewrites the header size and SHA-1 id):
#   - deletes /reserved-memory/consys-reserve-memory and /reserved-memory/secure-carveout, which
#     hold RAM back from the kernel (the MT8163 connectivity subsystem's and a secure-world
#     carveout this kernel never uses);
#   - deletes amzn,mic-downmix from /soc/spi@1100a000/spi@0 so the SPI capture driver hands over
#     two real microphone channels.
# atf-reserved-memory, ram_console-reserved-memory, pstore-reserved-memory and
# minirdump-reserved-memory are left alone: the bootloader and the crash record use them.
set -eu
[ $# -eq 2 ] || { echo "usage: $0 <boot_in.img> <boot_out.img>" >&2; exit 2; }
HERE=$(cd "$(dirname "$0")" && pwd)
exec python3 "$HERE/patch-dtb.py" "$1" "$2" \
	--delete-node /reserved-memory/consys-reserve-memory \
	--delete-node /reserved-memory/secure-carveout \
	--delete /soc/spi@1100a000/spi@0 amzn,mic-downmix
