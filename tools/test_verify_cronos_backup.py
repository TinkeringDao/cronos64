#!/usr/bin/env python3
"""Offline tests for tools/verify-cronos-backup.py (no hardware, no network)."""

import gzip
import hashlib
import importlib.util
import os
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import unittest
import zlib

HERE = Path(__file__).resolve().parent
SCRIPT_PATH = HERE / "verify-cronos-backup.py"


def load_verifier():
    spec = importlib.util.spec_from_file_location("verify_cronos_backup", SCRIPT_PATH)
    if spec is None or spec.loader is None:
        raise ImportError(f"cannot load {SCRIPT_PATH}")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


vcb = load_verifier()

PART_NAMES = {
    1: "proinfo",
    2: "nvram",
    3: "lk",
    4: "protect1",
    5: "protect2",
    6: "kb",
    7: "dkb",
    8: "lk2",
    9: "boot",
    10: "recovery",
    11: "tee1",
    14: "logo",
    15: "misc",
}


def make_boot_image(
    kernel_payload: bytes,
    magic: bytes = b"ANDROID!",
    page_size: int = 2048,
    *,
    ramdisk_payload: bytes = b"",
    declared_kernel_size: int | None = None,
    declared_ramdisk_size: int | None = None,
    declared_second_size: int = 0,
) -> bytes:
    header_size = max(page_size, 44)
    header = bytearray(header_size)
    header[: len(magic)] = magic
    ks = len(kernel_payload) if declared_kernel_size is None else declared_kernel_size
    rs = len(ramdisk_payload) if declared_ramdisk_size is None else declared_ramdisk_size
    struct.pack_into(
        "<9I",
        header,
        8,
        ks,                   # kernel_size
        0x40080000,           # kernel_addr
        rs,                   # ramdisk_size
        0x44000000,           # ramdisk_addr
        declared_second_size, # second_size
        0x40F00000,           # second_addr
        0x40000100,           # tags_addr
        page_size,            # page_size
        0,                    # header_version
    )
    kpad = (-len(kernel_payload)) % page_size if page_size > 0 else 0
    rpad = (-len(ramdisk_payload)) % page_size if page_size > 0 else 0
    return bytes(header) + kernel_payload + (b"\x00" * kpad) + ramdisk_payload + (b"\x00" * rpad)


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def build_backup_dir(
    root: Path,
    *,
    plain_names: bool = False,
    boot_aliases: tuple[str, str] = ("mmcblk0boot0.img", "mmcblk0boot1.img"),
    kernel_mode: str = "gzip_dtb",
    kernel_banner: bytes = b"Linux version 4.9.337-g8d928c5176cc (builder@local) #1 SMP PREEMPT",
    p9_magic: bytes = b"ANDROID!",
    include_boot_in_sums: bool = False,
) -> Path:
    root.mkdir(parents=True, exist_ok=True)

    if kernel_mode == "gzip_dtb":
        kernel_payload = gzip.compress(b"\x00\x00" + kernel_banner + b"\x00") + b"\xd0\x0d\xfe\xed" + b"\x00" * 64
    elif kernel_mode == "zlib":
        kernel_payload = zlib.compress(b"\x00" + kernel_banner + b"\x00")
    elif kernel_mode == "raw":
        kernel_payload = b"\x00" * 128 + kernel_banner + b"\x00" * 128
    else:
        raise ValueError(f"unknown kernel_mode {kernel_mode}")

    sums_lines = []
    for num, part_name in PART_NAMES.items():
        if plain_names:
            fname = f"p{num}.img"
        elif num == 8:
            fname = "p8-part.img"
        else:
            fname = f"p{num}-{part_name}.img"

        if num == 9:
            data = make_boot_image(kernel_payload, magic=p9_magic)
        else:
            data = f"synthetic-{part_name}-partition-{num}\n".encode() * 16

        fpath = root / fname
        fpath.write_bytes(data)
        sums_lines.append(f"{sha256_bytes(data)}  {fname}")

    for idx, alias in enumerate(boot_aliases):
        bdata = f"synthetic-emmc-boot{idx}-chain\n".encode() * 32
        bpath = root / alias
        bpath.write_bytes(bdata)
        if include_boot_in_sums:
            sums_lines.append(f"{sha256_bytes(bdata)}  {alias}")

    (root / "SHA256SUMS").write_text("\n".join(sums_lines) + "\n", encoding="utf-8")
    return root


def run_cli(backup_dir: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, str(SCRIPT_PATH), str(backup_dir)],
        capture_output=True,
        text=True,
        check=False,
    )


class VerifyCronosBackupTest(unittest.TestCase):
    def test_valid_install_show_backup_gzip_kernel_and_idempotent_boot_hashes(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "backup", kernel_mode="gzip_dtb")
            sums_before = (bdir / "SHA256SUMS").read_text(encoding="utf-8")
            self.assertNotIn("mmcblk0boot0.img", sums_before)
            self.assertNotIn("mmcblk0boot1.img", sums_before)

            res1 = run_cli(bdir)
            self.assertEqual(res1.returncode, 0, msg=f"stderr: {res1.stderr}\nstdout: {res1.stdout}")

            sums_after1 = (bdir / "SHA256SUMS").read_text(encoding="utf-8")
            expected_b0 = f"{sha256_bytes((bdir / 'mmcblk0boot0.img').read_bytes())}  mmcblk0boot0.img"
            expected_b1 = f"{sha256_bytes((bdir / 'mmcblk0boot1.img').read_bytes())}  mmcblk0boot1.img"
            self.assertIn(expected_b0, sums_after1.splitlines())
            self.assertIn(expected_b1, sums_after1.splitlines())

            # Second run must be idempotent without duplicating boot0/boot1 entries.
            res2 = run_cli(bdir)
            self.assertEqual(res2.returncode, 0, msg=f"stderr: {res2.stderr}")
            sums_after2 = (bdir / "SHA256SUMS").read_text(encoding="utf-8")
            self.assertEqual(sums_after1, sums_after2)

    def test_valid_plain_names_boot_aliases_and_raw_kernel(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(
                Path(tmp) / "plain",
                plain_names=True,
                boot_aliases=("boot0.img", "boot1"),
                kernel_mode="raw",
            )
            report = vcb.verify_backup(bdir)
            self.assertTrue(report["ok"])
            self.assertEqual(sorted(report["appended"]), ["boot0.img", "boot1"])

            res = run_cli(bdir)
            self.assertEqual(res.returncode, 0, msg=res.stderr)
            lines = (bdir / "SHA256SUMS").read_text(encoding="utf-8").splitlines()
            self.assertEqual(len(lines), 15)

    def test_valid_zlib_kernel(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "zlib", kernel_mode="zlib")
            res = run_cli(bdir)
            self.assertEqual(res.returncode, 0, msg=res.stderr)

    def test_missing_p3_lk_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "missing_p3")
            (bdir / "p3-lk.img").unlink()
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("p3", res.stderr.lower())

    def test_missing_other_required_partition_or_boot_rejected(self):
        for target in ("p14-logo.img", "mmcblk0boot0.img", "mmcblk0boot1.img"):
            with self.subTest(target=target), tempfile.TemporaryDirectory() as tmp:
                bdir = build_backup_dir(Path(tmp) / "missing")
                (bdir / target).unlink()
                res = run_cli(bdir)
                self.assertNotEqual(res.returncode, 0)
                self.assertTrue(res.stderr.strip(), "expected explicit stderr diagnostic")

    def test_zero_byte_boot0_and_p3_rejected(self):
        for target in ("mmcblk0boot0.img", "p3-lk.img"):
            with self.subTest(target=target), tempfile.TemporaryDirectory() as tmp:
                bdir = build_backup_dir(Path(tmp) / "empty")
                target_path = bdir / target
                target_path.write_bytes(b"")
                # Keep SHA256SUMS matching for p3 to isolate the zero-byte check.
                if target == "p3-lk.img":
                    sums = (bdir / "SHA256SUMS").read_text(encoding="utf-8")
                    old_line = [ln for ln in sums.splitlines() if ln.endswith("p3-lk.img")][0]
                    new_line = f"{sha256_bytes(b'')}  p3-lk.img"
                    (bdir / "SHA256SUMS").write_text(sums.replace(old_line, new_line), encoding="utf-8")

                res = run_cli(bdir)
                self.assertNotEqual(res.returncode, 0)
                self.assertRegex(res.stderr.lower(), r"empty|zero")

    def test_p9_missing_android_magic_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "bad_magic", p9_magic=b"NOTBOOT!")
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("ANDROID!", res.stderr)

    def test_p9_missing_kernel_4_9_337_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(
                Path(tmp) / "bad_kver",
                kernel_banner=b"Linux version 5.10.198 (builder@local)",
            )
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("4.9.337", res.stderr)

    def test_sha256_mismatch_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "corrupt")
            p1 = bdir / "p1-proinfo.img"
            p1.write_bytes(p1.read_bytes() + b"corrupted")
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("p1-proinfo.img", res.stderr)
            self.assertRegex(res.stderr.lower(), r"mismatch|checksum|sha256")

    def test_existing_boot0_sha256_mismatch_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "boot_mismatch", include_boot_in_sums=True)
            (bdir / "mmcblk0boot0.img").write_bytes(b"tampered-boot0-data")
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("mmcblk0boot0.img", res.stderr)

    def test_missing_sha256sums_entry_for_required_partition_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "missing_entry")
            lines = (bdir / "SHA256SUMS").read_text(encoding="utf-8").splitlines()
            lines = [ln for ln in lines if not ln.endswith("p9-boot.img")]
            (bdir / "SHA256SUMS").write_text("\n".join(lines) + "\n", encoding="utf-8")
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("p9-boot.img", res.stderr)

    def test_malformed_sha256sums_line_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "malformed")
            with (bdir / "SHA256SUMS").open("a", encoding="utf-8") as fh:
                fh.write("not-a-valid-sha256-line\n")
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertRegex(res.stderr.lower(), r"malformed|sha256sums")

    def test_duplicate_partition_slot_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "dup_slot")
            dup = bdir / "p3.img"
            dup.write_bytes(b"duplicate")
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("p3", res.stderr.lower())

    @staticmethod
    def _rewrite_p9(bdir: Path, payload: bytes) -> None:
        (bdir / "p9-boot.img").write_bytes(payload)
        lines = (bdir / "SHA256SUMS").read_text(encoding="utf-8").splitlines()
        new_lines = [
            f"{sha256_bytes(payload)}  p9-boot.img" if ln.endswith("p9-boot.img") else ln
            for ln in lines
        ]
        (bdir / "SHA256SUMS").write_text("\n".join(new_lines) + "\n", encoding="utf-8")

    def test_p9_truncated_header_and_section_bounds_rejected(self):
        valid_kernel = gzip.compress(b"\x00Linux version 4.9.337-g8d928c5176cc\x00")
        cases = {
            "short_header": b"ANDROID!" + b"\x00" * 16,
            "kernel_exceeds_file": make_boot_image(
                b"\x00Linux version 4.9.337\x00",
                declared_kernel_size=65536,
            ),
            "ramdisk_exceeds_file": make_boot_image(
                valid_kernel,
                declared_ramdisk_size=65536,
            ),
            "second_exceeds_file": make_boot_image(
                valid_kernel,
                declared_second_size=65536,
            ),
            "zero_page_size": make_boot_image(
                b"\x00Linux version 4.9.337\x00",
                page_size=0,
            ),
            "zero_kernel_size": make_boot_image(
                b"\x00Linux version 4.9.337\x00",
                declared_kernel_size=0,
            ),
            "banner_only_in_ramdisk": make_boot_image(
                b"\x00" * 128,
                ramdisk_payload=b"Linux version 4.9.337",
            ),
        }
        for label, payload in cases.items():
            with self.subTest(case=label), tempfile.TemporaryDirectory() as tmp:
                bdir = build_backup_dir(Path(tmp) / label)
                self._rewrite_p9(bdir, payload)
                res = run_cli(bdir)
                self.assertNotEqual(res.returncode, 0, msg=f"{label} unexpectedly passed")
                self.assertTrue(res.stderr.strip(), msg=f"{label} missing stderr diagnostic")

    def test_p9_truncated_gzip_trailer_rejected(self):
        full_gz = gzip.compress(b"\x00Linux version 4.9.337-g8d928c5176cc\x00" * 4)
        truncated_gz = full_gz[:-8]
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "trunc_gz")
            self._rewrite_p9(bdir, make_boot_image(truncated_gz))
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("4.9.337", res.stderr)

    def test_missing_sha256sums_manifest_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "no_sums")
            (bdir / "SHA256SUMS").unlink()
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("SHA256SUMS", res.stderr)

    def test_conflicting_sha256sums_entries_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "conflict_sums")
            with (bdir / "SHA256SUMS").open("a", encoding="utf-8") as fh:
                fh.write("0" * 64 + "  p1-proinfo.img\n")
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("p1-proinfo.img", res.stderr)

    def test_duplicate_boot_slot_files_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "dup_boot")
            (bdir / "boot0.img").write_bytes(b"duplicate-boot0")
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("boot0", res.stderr.lower())

    def test_auxiliary_non_image_boot_files_ignored_and_rejected_alone(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "aux_boot")
            (bdir / "boot0.txt").write_text("note\n", encoding="utf-8")
            (bdir / "mmcblk0boot0.img.bak").write_bytes(b"bak")
            (bdir / "boot0.sha256").write_text("0" * 64 + "\n", encoding="utf-8")
            res = run_cli(bdir)
            self.assertEqual(res.returncode, 0, msg=res.stderr)

        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "only_txt_boot")
            (bdir / "mmcblk0boot0.img").unlink()
            (bdir / "boot0.txt").write_text("not-an-image\n", encoding="utf-8")
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("boot0", res.stderr.lower())

    def test_non_utf8_sha256sums_rejected_cleanly(self):
        with tempfile.TemporaryDirectory() as tmp:
            bdir = build_backup_dir(Path(tmp) / "bad_utf8")
            (bdir / "SHA256SUMS").write_bytes(b"\xff\xfe\xfd\x80\n")
            res = run_cli(bdir)
            self.assertNotEqual(res.returncode, 0)
            self.assertIn("error:", res.stderr.lower())
            self.assertNotIn("Traceback", res.stderr)


if __name__ == "__main__":
    unittest.main()
