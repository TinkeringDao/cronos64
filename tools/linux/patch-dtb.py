#!/usr/bin/env python3
"""Edit the device trees appended to an Android boot image's kernel.

The LineageOS cronos kernel is Image.gz with eleven flattened device trees
appended (one per board revision; the bootloader picks by board id). This
rewrites every one of them and writes a new boot image with the same header,
ramdisk and command line.

    python3 patch-dtb.py boot.img out.img --delete /soc/spi@1100a000/spi@0 amzn,mic-downmix
    python3 patch-dtb.py boot.img out.img --delete-node /reserved-memory/secure-carveout

The first use: `amzn,mic-downmix` makes Amazon's SPI capture driver average the
two microphone channels into both slots (amzn-mt-spi-pcm.c); without it the
daemon gets two real microphones. Needs the `fdt` package (pip install fdt).
"""
import argparse
import hashlib
import struct
import sys
import zlib

import fdt

DTB_MAGIC = b"\xd0\x0d\xfe\xed"


def split_kernel(kernel):
    """Return (gzip stream, [dtb blobs]) for an Image.gz-dtb kernel."""
    do = zlib.decompressobj(16 + zlib.MAX_WBITS)
    do.decompress(kernel)
    tail = do.unused_data
    gz = kernel[: len(kernel) - len(tail)]
    dtbs = []
    i = 0
    while True:
        j = tail.find(DTB_MAGIC, i)
        if j < 0:
            break
        size = struct.unpack(">I", tail[j + 4 : j + 8])[0]
        dtbs.append(tail[j : j + size])
        i = j + size
    if not dtbs:
        sys.exit("no device tree appended to the kernel")
    return gz, dtbs


def find_node(dt, path):
    """fdt raises ValueError for a path that is not there; a device tree without it is skipped."""
    try:
        return dt.get_node(path)
    except ValueError:
        return None


def edit(blob, deletes, delete_nodes, sets):
    dt = fdt.parse_dtb(blob)
    changed = 0
    for node_path in delete_nodes:
        parent_path, _, child_name = node_path.rstrip("/").rpartition("/")
        parent = find_node(dt, parent_path or "/")
        if parent is None:
            continue
        if any(s.name == child_name for s in parent.nodes):
            parent.remove_subnode(child_name)
            changed += 1
    for path, prop in deletes:
        node = find_node(dt, path)
        if node is None:
            continue
        if node.get_property(prop) is not None:
            node.remove_property(prop)
            changed += 1
    for path, prop, value in sets:
        node = find_node(dt, path)
        if node is None:
            continue
        node.set_property(prop, value)
        changed += 1
    return dt.to_dtb(version=17), changed


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("boot_in")
    ap.add_argument("boot_out")
    ap.add_argument("--delete", nargs=2, action="append", default=[], metavar=("NODE", "PROP"),
                    help="remove a property from a node, in every appended device tree")
    ap.add_argument("--delete-node", action="append", default=[], metavar="NODE",
                    help="remove a whole node (and its children), in every appended device tree")
    ap.add_argument("--set-u32", nargs=3, action="append", default=[], metavar=("NODE", "PROP", "VALUE"),
                    help="set a 32-bit property")
    a = ap.parse_args()

    d = open(a.boot_in, "rb").read()
    if d[:8] != b"ANDROID!":
        # A raw Image.gz-dtb (build-kernel.sh output): patch it in place, no boot image around it.
        gz, dtbs = split_kernel(d)
        sets = [(n, p, int(v, 0)) for n, p, v in a.set_u32]
        out, total = [], 0
        for i, blob in enumerate(dtbs):
            new, changed = edit(blob, a.delete, a.delete_node, sets)
            total += changed
            out.append(new)
        if total == 0:
            sys.exit("nothing changed; check the node path and property name")
        open(a.boot_out, "wb").write(gz + b"".join(out))
        print(f"wrote {a.boot_out}: raw kernel, {len(dtbs)} device trees, {total} changes")
        return
    ks, ka, rs, ra, ss, sa, tl, ps, hv = struct.unpack("<9I", d[8:44])
    pg = lambda n: ((n + ps - 1) // ps) * ps
    hdr = bytearray(d[:ps])
    kernel = d[ps : ps + ks]
    ramdisk = d[ps + pg(ks) : ps + pg(ks) + rs]

    gz, dtbs = split_kernel(kernel)
    sets = [(n, p, int(v, 0)) for n, p, v in a.set_u32]
    out = []
    total = 0
    for i, blob in enumerate(dtbs):
        new, changed = edit(blob, a.delete, a.delete_node, sets)
        total += changed
        print(f"dtb {i}: {len(blob)} -> {len(new)} bytes, {changed} change(s)")
        out.append(new)
    if total == 0:
        sys.exit("nothing changed; check the node path and property name")

    kernel = gz + b"".join(out)
    struct.pack_into("<I", hdr, 8, len(kernel))
    h = hashlib.sha1()
    for blob in (kernel, ramdisk, b"", b""):
        h.update(blob)
        h.update(struct.pack("<I", len(blob)))
    hdr[576 : 576 + 32] = h.digest() + b"\0" * 12
    img = bytes(hdr) + kernel + b"\0" * (pg(len(kernel)) - len(kernel)) + ramdisk + b"\0" * (pg(len(ramdisk)) - len(ramdisk))
    open(a.boot_out, "wb").write(img)
    print(f"wrote {a.boot_out}: kernel {len(kernel)} bytes, {len(dtbs)} device trees, {total} changes")


if __name__ == "__main__":
    main()
