# TECHO5: runtime tuning for the Echo Show 5 (cronos) on the LineageOS 4.9 kernel. Sourced by
# /etc/techo5/boot.sh. The kernel is Amazon's stock configuration plus what the boot image's device
# tree gives it, so memory and the CPU governor are tuned from userspace.
#
# SYS_ROOT, PROC_ROOT and DEV_ROOT stand in for /sys, /proc and /dev (tests point them at a fake tree).

t5_unthrottle_say() { if command -v log > /dev/null 2>&1; then log "$*"; else echo "techo5-unthrottle: $*" >&2; fi; }

# t5_unthrottle_w <file> <value>: best effort, logged.
t5_unthrottle_w() {
	[ -e "$1" ] || { t5_unthrottle_say "tune: no ${1##*/}"; return 1; }
	echo "$2" > "$1" 2> /dev/null || { t5_unthrottle_say "tune: ${1##*/} not set to $2"; return 1; }
}

t5_cronos_unthrottle() {
	SYS_ROOT=${SYS_ROOT:-/sys}
	PROC_ROOT=${PROC_ROOT:-/proc}
	DEV_ROOT=${DEV_ROOT:-/dev}
	grep -q 'amazon,cronos' "$PROC_ROOT/device-tree/compatible" 2> /dev/null || return 0

	# ZRAM: 384 MiB of lz4 swap. The 1 GiB board is short of RAM with the daemon, the camera and the
	# browser up; compressed swap costs far less than refaulting pages from eMMC. page-cluster 0
	# reads back one page at a time, which is what zram wants.
	if [ ! -d "$SYS_ROOT/block/zram0" ] && command -v modprobe > /dev/null 2>&1; then
		modprobe zram 2> /dev/null || true
	fi
	if [ -d "$SYS_ROOT/block/zram0" ] && ! grep -q zram0 "$PROC_ROOT/swaps" 2> /dev/null; then
		t5_unthrottle_w "$SYS_ROOT/block/zram0/comp_algorithm" lz4
		if t5_unthrottle_w "$SYS_ROOT/block/zram0/disksize" $((384 * 1024 * 1024)) &&
			mkswap "$DEV_ROOT/zram0" > /dev/null 2>&1 &&
			swapon -p 100 "$DEV_ROOT/zram0" > /dev/null 2>&1; then
			t5_unthrottle_w "$PROC_ROOT/sys/vm/swappiness" 100
			t5_unthrottle_w "$PROC_ROOT/sys/vm/page-cluster" 0
		else
			t5_unthrottle_say "zram: swap not set up"
		fi
	fi

	# eMMC: deadline with a 512 KiB read-ahead.
	q=$SYS_ROOT/block/mmcblk0/queue
	t5_unthrottle_w "$q/scheduler" deadline
	t5_unthrottle_w "$q/read_ahead_kb" 512

	# CPU: the 4.9 interactive governor, ramping to 1.3 GHz (the FY-binned MT8163's top OPP) at 85%
	# load, sampled every 20 ms and held for 40 ms. The OPP table itself is left as the board has it.
	c=$SYS_ROOT/devices/system/cpu/cpufreq
	for p in "$c"/policy*; do
		[ -e "$p/scaling_governor" ] || continue
		grep -qw interactive "$p/scaling_available_governors" 2> /dev/null &&
			t5_unthrottle_w "$p/scaling_governor" interactive
	done
	g=
	for d in "$c/interactive" "$c"/policy0/interactive; do
		[ -d "$d" ] && { g=$d; break; }
	done
	if [ -n "$g" ]; then
		t5_unthrottle_w "$g/hispeed_freq" 1300000
		t5_unthrottle_w "$g/go_hispeed_load" 85
		t5_unthrottle_w "$g/timer_rate" 20000
		t5_unthrottle_w "$g/min_sample_time" 40000
	else
		t5_unthrottle_say "cpufreq: no interactive governor"
	fi
	return 0
}
