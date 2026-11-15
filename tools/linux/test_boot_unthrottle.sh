#!/bin/sh
# Hermetic test for t5_cronos_unthrottle (rootfs/lib/techo5-unthrottle.sh): fake /sys, /proc, mock tools.
HERE=$(cd "$(dirname "$0")" && pwd)
T=$(mktemp -d) || exit 1
trap 'rm -rf "$T"' EXIT
fail=0
check() { # name actual expected
	if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2', want '$3'"; fail=1; fi
}

SYS_ROOT=$T/sys PROC_ROOT=$T/proc DEV_ROOT=$T/dev
export SYS_ROOT PROC_ROOT DEV_ROOT
mkdir -p "$SYS_ROOT/block/zram0" "$SYS_ROOT/block/mmcblk0/queue" \
	"$SYS_ROOT/devices/system/cpu/cpufreq/interactive" "$PROC_ROOT/device-tree" \
	"$PROC_ROOT/sys/vm" "$DEV_ROOT" "$T/bin"
printf 'amazon,cronos\0amazon,mt8163\0' > "$PROC_ROOT/device-tree/compatible"
: > "$DEV_ROOT/zram0"
: > "$PROC_ROOT/sys/vm/swappiness"
: > "$PROC_ROOT/sys/vm/page-cluster"
G=$SYS_ROOT/devices/system/cpu/cpufreq/interactive
: > "$SYS_ROOT/block/zram0/comp_algorithm"
: > "$SYS_ROOT/block/zram0/disksize"
for f in hispeed_freq go_hispeed_load timer_rate min_sample_time; do : > "$G/$f"; done
echo "[mq-deadline] none" > "$SYS_ROOT/block/mmcblk0/queue/scheduler"
: > "$SYS_ROOT/block/mmcblk0/queue/read_ahead_kb"
mkdir -p "$SYS_ROOT/devices/system/cpu/cpufreq/policy0"
echo mt8163-cpufreq > "$SYS_ROOT/devices/system/cpu/cpufreq/policy0/scaling_driver"
for tool in mkswap swapon sysctl; do
	printf '#!/bin/sh\necho "%s $*" >> "%s/calls"\n' "$tool" "$T" > "$T/bin/$tool"
	chmod +x "$T/bin/$tool"
done
PATH=$T/bin:$PATH
export PATH
log() { :; }

if [ ! -r "$HERE/rootfs/lib/techo5-unthrottle.sh" ]; then
	echo "FAIL: missing $HERE/rootfs/lib/techo5-unthrottle.sh"
	exit 1
fi
. "$HERE/rootfs/lib/techo5-unthrottle.sh"
t5_cronos_unthrottle

check zram.comp_algorithm "$(cat "$SYS_ROOT/block/zram0/comp_algorithm")" lz4
check zram.disksize "$(cat "$SYS_ROOT/block/zram0/disksize")" 402653184
check mkswap.called "$(grep -c '^mkswap .*zram0' "$T/calls")" 1
check swapon.called "$(grep -c '^swapon .*zram0' "$T/calls")" 1
check vm.swappiness "$(cat "$PROC_ROOT/sys/vm/swappiness")" 100
check vm.page-cluster "$(cat "$PROC_ROOT/sys/vm/page-cluster")" 0
check mmc.scheduler "$(cat "$SYS_ROOT/block/mmcblk0/queue/scheduler")" deadline
check mmc.read_ahead_kb "$(cat "$SYS_ROOT/block/mmcblk0/queue/read_ahead_kb")" 512
check gov.hispeed_freq "$(cat "$G/hispeed_freq")" 1300000
check gov.go_hispeed_load "$(cat "$G/go_hispeed_load")" 85
check gov.timer_rate "$(cat "$G/timer_rate")" 20000
check gov.min_sample_time "$(cat "$G/min_sample_time")" 40000

# Gate: cronos on the mainline kernel (mtk-cpufreq) is left alone.
rm -f "$T/calls"
: > "$SYS_ROOT/block/zram0/comp_algorithm"
echo mtk-cpufreq > "$SYS_ROOT/devices/system/cpu/cpufreq/policy0/scaling_driver"
echo "[mq-deadline] none" > "$SYS_ROOT/block/mmcblk0/queue/scheduler"
t5_cronos_unthrottle
check gate.mainline.zram "$(cat "$SYS_ROOT/block/zram0/comp_algorithm")" ""
check gate.mainline.scheduler "$(cat "$SYS_ROOT/block/mmcblk0/queue/scheduler")" "[mq-deadline] none"
[ ! -e "$T/calls" ]
check gate.mainline.no-tools $? 0
echo mt8163-cpufreq > "$SYS_ROOT/devices/system/cpu/cpufreq/policy0/scaling_driver"

# Gate: a device that is not cronos is left alone.
rm -f "$T/calls" "$SYS_ROOT/block/zram0/comp_algorithm"
printf 'amzn,athena\0' > "$PROC_ROOT/device-tree/compatible"
t5_cronos_unthrottle
[ ! -e "$SYS_ROOT/block/zram0/comp_algorithm" ] && [ ! -e "$T/calls" ]
check gate.non-cronos $? 0

# Gate: 4.9.337 cronos DTB reports mediatek,mt8163 in compatible and androidboot.product=cronos in cmdline.
: > "$SYS_ROOT/block/zram0/comp_algorithm"
printf 'mediatek,mt8163\0' > "$PROC_ROOT/device-tree/compatible"
echo "console=tty0 androidboot.product=cronos techo5=linux" > "$PROC_ROOT/cmdline"
t5_cronos_unthrottle
check gate.cmdline-cronos.zram "$(cat "$SYS_ROOT/block/zram0/comp_algorithm")" lz4

[ $fail -eq 0 ] && echo "PASS" || { echo "FAILED"; exit 1; }
