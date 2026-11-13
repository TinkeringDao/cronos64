#!/usr/bin/env python3
"""Round-trip test for unthrottle-cronos-boot.sh / patch-dtb.py on a synthetic boot image."""
import gzip
import hashlib
import os
import struct
import subprocess
import tempfile
import unittest

import fdt

HERE = os.path.dirname(os.path.abspath(__file__))
PAGE = 4096
DTB_MAGIC = b"\xd0\x0d\xfe\xed"


def make_dtb(rev):
    dt = fdt.FDT()
    root = dt.root
    root.append(fdt.PropStrings("compatible", "amazon,cronos"))
    root.append(fdt.PropWords("board-rev", rev))
    rm = fdt.Node("reserved-memory")
    rm.append(fdt.PropWords("#address-cells", 2))
    rm.append(fdt.PropWords("#size-cells", 2))
    rm.append(fdt.PropBytes("ranges"))
    for name, base in (
        ("atf-reserved-memory", 0x43000000),
        ("pstore-reserved-memory", 0x44000000),
        ("consys-reserve-memory", 0x45000000),
        ("secure-carveout", 0x46000000),
    ):
        n = fdt.Node(name)
        n.append(fdt.PropWords("reg", 0, base, 0, 0x10000))
        rm.append(n)
    dt.add_item(rm)
    soc = fdt.Node("soc")
    spi_host = fdt.Node("spi@1100a000")
    spi0 = fdt.Node("spi@0")
    spi0.append(fdt.PropStrings("compatible", "amzn,mt-spi-pcm"))
    spi0.append(fdt.PropBytes("amzn,mic-downmix"))
    spi_host.append(spi0)
    soc.append(spi_host)
    dt.add_item(soc)
    return dt.to_dtb(version=17)


def make_boot_image(path, ndtb=2):
    gz = gzip.compress(b"synthetic kernel Image" * 64, mtime=0)
    kernel = gz + b"".join(make_dtb(i) for i in range(ndtb))
    ramdisk = b"synthetic ramdisk" * 100
    hdr = bytearray(PAGE)
    hdr[:8] = b"ANDROID!"
    struct.pack_into("<9I", hdr, 8, len(kernel), 0x40008000, len(ramdisk), 0x45000000, 0, 0x40f00000, 0x44000000, PAGE, 0)
    pg = lambda n: ((n + PAGE - 1) // PAGE) * PAGE
    img = bytes(hdr) + kernel + b"\0" * (pg(len(kernel)) - len(kernel)) + ramdisk + b"\0" * (pg(len(ramdisk)) - len(ramdisk))
    with open(path, "wb") as f:
        f.write(img)


def parse_boot(path):
    with open(path, "rb") as f:
        d = f.read()
    ks, _, rs, _, _, _, _, ps, _ = struct.unpack("<9I", d[8:44])
    pg = lambda n: ((n + ps - 1) // ps) * ps
    kernel = d[ps : ps + ks]
    ramdisk = d[ps + pg(ks) : ps + pg(ks) + rs]
    return d[:ps], kernel, ramdisk


def appended_dtbs(kernel):
    import zlib

    do = zlib.decompressobj(16 + zlib.MAX_WBITS)
    do.decompress(kernel)
    tail = do.unused_data
    out, i = [], 0
    while True:
        j = tail.find(DTB_MAGIC, i)
        if j < 0:
            return out
        size = struct.unpack(">I", tail[j + 4 : j + 8])[0]
        out.append(tail[j : j + size])
        i = j + size


class UnthrottleBootTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.src = os.path.join(self.tmp.name, "in.img")
        self.dst = os.path.join(self.tmp.name, "out.img")
        make_boot_image(self.src)

    def tearDown(self):
        self.tmp.cleanup()

    def run_script(self):
        return subprocess.run(
            ["sh", os.path.join(HERE, "unthrottle-cronos-boot.sh"), self.src, self.dst],
            capture_output=True, text=True,
        )

    def test_unthrottle_round_trip(self):
        r = self.run_script()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        self.assertIn("2 device trees, 6 changes", r.stdout)
        hdr, kernel, ramdisk = parse_boot(self.dst)
        dtbs = appended_dtbs(kernel)
        self.assertEqual(len(dtbs), 2)
        for blob in dtbs:
            dt = fdt.parse_dtb(blob)
            names = [n.name for n in dt.get_node("/reserved-memory").nodes]
            self.assertNotIn("consys-reserve-memory", names)
            self.assertNotIn("secure-carveout", names)
            self.assertIn("atf-reserved-memory", names)
            self.assertIn("pstore-reserved-memory", names)
            spi0 = dt.get_node("/soc/spi@1100a000/spi@0")
            self.assertIsNone(spi0.get_property("amzn,mic-downmix"))
            self.assertIsNotNone(spi0.get_property("compatible"))

    def test_header_size_and_sha1(self):
        r = self.run_script()
        self.assertEqual(r.returncode, 0, r.stdout + r.stderr)
        hdr, kernel, ramdisk = parse_boot(self.dst)
        self.assertEqual(struct.unpack("<I", hdr[8:12])[0], len(kernel))
        h = hashlib.sha1()
        for blob in (kernel, ramdisk, b"", b""):
            h.update(blob)
            h.update(struct.pack("<I", len(blob)))
        self.assertEqual(hdr[576:596], h.digest())
        self.assertEqual(ramdisk, b"synthetic ramdisk" * 100)

    def test_idempotent_second_run_changes_nothing(self):
        self.assertEqual(self.run_script().returncode, 0)
        self.src, self.dst = self.dst, os.path.join(self.tmp.name, "out2.img")
        r = self.run_script()
        self.assertNotEqual(r.returncode, 0)
        self.assertIn("nothing changed", r.stdout + r.stderr)


if __name__ == "__main__":
    unittest.main()
