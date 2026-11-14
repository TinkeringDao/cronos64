#!/usr/bin/env python3
"""Offline verifier for Cronos partition backups created by tools/install-show.py.

Usage:
    python3 tools/verify-cronos-backup.py <backup_dir>
"""

import argparse
import hashlib
import os
from pathlib import Path
import re
import stat
import struct
import sys
import tempfile
import zlib

REQUIRED_PARTITIONS = (1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 14, 15)
REQUIRED_KERNEL_BANNER = b"Linux version 4.9.337"
FIREOS_KERNEL_BANNER = b"Linux version 4.9."
ANDROID_BOOT_MAGIC = b"ANDROID!"

PART_FILE_RE = re.compile(r"^p(\d+)(?:-[^/]+)?\.img$")
BOOT_FILE_RE = re.compile(r"^(?:mmcblk0)?boot([01])(?:-[^/]+)?(?:\.img)?$")
SHA256_LINE_RE = re.compile(r"^([0-9a-fA-F]{64}) [ *](.+)$")

GZIP_SIGNATURES = (b"\x1f\x8b\x08",)
ZLIB_SIGNATURES = (b"\x78\x9c", b"\x78\xda", b"\x78\x01", b"\x78\x5e")


class BackupVerificationError(Exception):
    """Raised when a partition backup directory fails an offline safety invariant."""


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as fh:
        for chunk in iter(lambda: fh.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def _try_decompress_contains(buf: bytes | memoryview, wbits: int, needle: bytes) -> bool:
    try:
        dec = zlib.decompressobj(wbits)
        out = dec.decompress(buf) + dec.flush()
    except zlib.error:
        return False
    if not dec.eof:
        return False
    return needle in out


def _scan_stream_contains(data: bytes, signatures: tuple[bytes, ...], wbits: int, needle: bytes) -> bool:
    for sig in signatures:
        start = 0
        while True:
            idx = data.find(sig, start)
            if idx < 0:
                break
            if _try_decompress_contains(memoryview(data)[idx:], wbits, needle):
                return True
            start = idx + 1
    return False


def _page_align(size: int, page_size: int) -> int:
    return ((size + page_size - 1) // page_size) * page_size


def _extract_bootimg_kernel(p9_name: str, data: bytes) -> bytes:
    if len(data) < 44:
        raise BackupVerificationError(
            f"{p9_name} (p9 boot): truncated Android boot image header ({len(data)} bytes < 44)"
        )
    kernel_size, _ka, ramdisk_size, _ra, second_size, _sa, _tl, page_size, _hv = struct.unpack_from(
        "<9I", data, 8
    )
    if page_size < 64 or page_size > len(data):
        raise BackupVerificationError(
            f"{p9_name} (p9 boot): invalid Android boot image page_size={page_size} for {len(data)}-byte image"
        )
    if kernel_size <= 0:
        raise BackupVerificationError(f"{p9_name} (p9 boot): invalid kernel_size={kernel_size}")
    expected_min_size = (
        page_size
        + _page_align(kernel_size, page_size)
        + _page_align(ramdisk_size, page_size)
        + _page_align(second_size, page_size)
    )
    if expected_min_size > len(data):
        raise BackupVerificationError(
            f"{p9_name} (p9 boot): truncated boot image (sections need {expected_min_size} bytes, got {len(data)})"
        )
    return data[page_size : page_size + kernel_size]


def verify_boot_image(p9_path: Path, *, allow_fireos_kernel: bool = False) -> None:
    data = p9_path.read_bytes()
    if not data.startswith(ANDROID_BOOT_MAGIC):
        raise BackupVerificationError(
            f"{p9_path.name} (p9 boot): missing {ANDROID_BOOT_MAGIC.decode('ascii')} boot image magic at offset 0"
        )

    kernel_blob = _extract_bootimg_kernel(p9_path.name, data)
    banner = FIREOS_KERNEL_BANNER if allow_fireos_kernel else REQUIRED_KERNEL_BANNER

    if banner in kernel_blob:
        return
    if _try_decompress_contains(kernel_blob, 16 + zlib.MAX_WBITS, banner):
        return
    if _try_decompress_contains(kernel_blob, zlib.MAX_WBITS, banner):
        return
    if _scan_stream_contains(kernel_blob, GZIP_SIGNATURES, 16 + zlib.MAX_WBITS, banner):
        return
    if _scan_stream_contains(kernel_blob, ZLIB_SIGNATURES, zlib.MAX_WBITS, banner):
        return

    raise BackupVerificationError(
        f"{p9_path.name} (p9 boot): kernel payload missing '{banner.decode('ascii')}'"
    )


def _discover_slot_files(backup_dir: Path) -> tuple[dict[int, Path], dict[str, Path]]:
    entries = sorted(p for p in backup_dir.iterdir() if p.is_file())
    part_matches: dict[int, list[Path]] = {n: [] for n in REQUIRED_PARTITIONS}
    boot_matches: dict[str, list[Path]] = {"boot0": [], "boot1": []}

    for path in entries:
        pm = PART_FILE_RE.match(path.name)
        if pm:
            num = int(pm.group(1))
            if num in part_matches:
                part_matches[num].append(path)
            continue
        bm = BOOT_FILE_RE.match(path.name)
        if bm:
            boot_matches[f"boot{bm.group(1)}"].append(path)

    parts: dict[int, Path] = {}
    for num in REQUIRED_PARTITIONS:
        found = part_matches[num]
        label = f"p{num} (lk)" if num == 3 else f"p{num}"
        if len(found) == 0:
            raise BackupVerificationError(f"missing required partition dump for {label} in {backup_dir}")
        if len(found) > 1:
            names = ", ".join(p.name for p in found)
            raise BackupVerificationError(f"ambiguous multiple dumps for {label}: {names}")
        if found[0].stat().st_size <= 0:
            raise BackupVerificationError(f"{found[0].name} ({label}) is empty (0 bytes)")
        parts[num] = found[0]

    boots: dict[str, Path] = {}
    for slot in ("boot0", "boot1"):
        found = boot_matches[slot]
        if len(found) == 0:
            raise BackupVerificationError(f"missing required eMMC boot partition dump for {slot} in {backup_dir}")
        if len(found) > 1:
            names = ", ".join(p.name for p in found)
            raise BackupVerificationError(f"ambiguous multiple dumps for {slot}: {names}")
        if found[0].stat().st_size <= 0:
            raise BackupVerificationError(f"{found[0].name} ({slot}) is empty (0 bytes)")
        boots[slot] = found[0]

    return parts, boots


def _parse_sha256sums(sums_path: Path) -> tuple[list[str], dict[str, str]]:
    if not sums_path.is_file():
        raise BackupVerificationError(f"missing SHA256SUMS manifest at {sums_path}")

    try:
        raw_lines = sums_path.read_text(encoding="utf-8").splitlines()
    except (UnicodeDecodeError, OSError) as exc:
        raise BackupVerificationError(f"unreadable SHA256SUMS manifest at {sums_path}: {exc}") from exc

    entries: dict[str, str] = {}
    for lineno, line in enumerate(raw_lines, start=1):
        if not line.strip():
            continue
        m = SHA256_LINE_RE.match(line)
        if not m:
            raise BackupVerificationError(f"malformed SHA256SUMS line {lineno}: {line!r}")
        digest = m.group(1).lower()
        name = m.group(2)
        if name.startswith("./"):
            name = name[2:]
        if not name or "/" in name or "\\" in name or name in (".", ".."):
            raise BackupVerificationError(f"malformed filename in SHA256SUMS line {lineno}: {m.group(2)!r}")
        if name in entries and entries[name] != digest:
            raise BackupVerificationError(f"conflicting SHA256SUMS entries for {name}")
        entries[name] = digest

    return raw_lines, entries


def _write_sha256sums_atomic(sums_path: Path, lines: list[str]) -> None:
    mode = stat.S_IMODE(sums_path.stat().st_mode)
    payload = "\n".join(lines) + "\n"
    tmp_path: str | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            dir=sums_path.parent,
            prefix=".SHA256SUMS.",
            delete=False,
        ) as fh:
            tmp_path = fh.name
            fh.write(payload)
            fh.flush()
            os.fsync(fh.fileno())
        os.chmod(tmp_path, mode)
        os.replace(tmp_path, sums_path)
    except OSError as exc:
        if tmp_path is not None:
            try:
                os.unlink(tmp_path)
            except OSError:
                pass
        raise BackupVerificationError(f"failed writing SHA256SUMS atomically: {exc}") from exc
    except BaseException:
        if tmp_path is not None:
            try:
                os.unlink(tmp_path)
            except OSError:
                pass
        raise


def verify_backup(backup_dir: str | Path, *, allow_fireos_kernel: bool = False) -> dict[str, object]:
    root = Path(backup_dir)
    if not root.is_dir():
        raise BackupVerificationError(f"backup directory does not exist: {root}")

    parts, boots = _discover_slot_files(root)

    # Validate Android boot image (p9) header, bounds, and Linux 4.9.x kernel payload.
    verify_boot_image(parts[9], allow_fireos_kernel=allow_fireos_kernel)

    sums_path = root / "SHA256SUMS"
    raw_lines, entries = _parse_sha256sums(sums_path)

    # Verify every file referenced in SHA256SUMS.
    computed: dict[str, str] = {}
    for name, expected_digest in entries.items():
        target = root / name
        if not target.is_file():
            raise BackupVerificationError(f"SHA256SUMS references missing file: {name}")
        actual_digest = sha256_file(target)
        computed[name] = actual_digest
        if actual_digest != expected_digest:
            raise BackupVerificationError(
                f"SHA-256 mismatch for {name}: expected {expected_digest}, got {actual_digest}"
            )

    # Every required non-boot partition p1..p15 must be covered by SHA256SUMS.
    for num in REQUIRED_PARTITIONS:
        fname = parts[num].name
        if fname not in entries:
            label = f"p{num} (lk)" if num == 3 else f"p{num}"
            raise BackupVerificationError(f"SHA256SUMS missing entry for required partition {label}: {fname}")

    # Verify boot0/boot1 hashes and idempotently append them if install-show.py omitted them.
    appended: list[str] = []
    new_lines = [ln for ln in raw_lines if ln.strip()]
    for slot in ("boot0", "boot1"):
        bpath = boots[slot]
        digest = computed.get(bpath.name) or sha256_file(bpath)
        if bpath.name not in entries:
            new_lines.append(f"{digest}  {bpath.name}")
            entries[bpath.name] = digest
            appended.append(bpath.name)

    if appended:
        _write_sha256sums_atomic(sums_path, new_lines)

    return {
        "ok": True,
        "backup_dir": str(root),
        "partitions": len(parts),
        "boot_partitions": len(boots),
        "appended": appended,
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Verify Cronos partition backup completeness, p3/p9 integrity, and SHA256SUMS."
    )
    parser.add_argument("backup_dir", help="Path to backup directory (e.g. backups/<serial>/partitions)")
    parser.add_argument(
        "--allow-fireos-kernel",
        action="store_true",
        help="Accept any Linux 4.9.x kernel banner in p9 (for stock Fire OS dumps taken before LineageOS).",
    )
    args = parser.parse_args(argv)

    try:
        res = verify_backup(args.backup_dir, allow_fireos_kernel=args.allow_fireos_kernel)
    except BackupVerificationError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    appended_note = f"; appended {', '.join(res['appended'])} to SHA256SUMS" if res["appended"] else ""
    print(
        f"verified {res['partitions']} partitions + {res['boot_partitions']} boot slots "
        f"in {res['backup_dir']}{appended_note}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
