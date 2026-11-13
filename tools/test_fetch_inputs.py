#!/usr/bin/env python3
"""Offline tests for tools/fetch-inputs.py's root filesystem architecture (--arch). No network: every
download is replaced by a recorder.

    python3 tools/test_fetch_inputs.py
"""
import importlib.util
import os
import sys
import tempfile
import unittest
from unittest import mock

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location('fetch_inputs', os.path.join(HERE, 'fetch-inputs.py'))
fi = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(fi)

WPA_SET = ('wpa_supplicant-2.9-r8.apk', 'libssl1.1-1.1.1o-r0.apk', 'libcrypto1.1-1.1.1o-r0.apk',
           'libnl3-3.5.0-r0.apk')


class ParseArch(unittest.TestCase):
    def test_show_defaults_to_armv7(self):
        self.assertEqual(fi.parse_args(['--device', 'show']).arch, 'armv7')

    def test_cronos_defaults_to_aarch64(self):
        self.assertEqual(fi.parse_args(['--device', 'cronos']).arch, 'aarch64')

    def test_explicit_arch_wins(self):
        self.assertEqual(fi.parse_args(['--device', 'show', '--arch', 'aarch64']).arch, 'aarch64')
        self.assertEqual(fi.parse_args(['--device', 'cronos', '--arch', 'armv7']).arch, 'armv7')

    def test_unknown_arch_is_refused(self):
        with mock.patch('sys.stderr'), self.assertRaises(SystemExit):
            fi.parse_args(['--arch', 'x86_64'])

    def test_aarch64_only_for_a_64_bit_kernel_board(self):
        # The Spot's and the Dot's kernels are 32-bit: an aarch64 userspace cannot run there.
        for dev in ('spot', 'dot'):
            with mock.patch('sys.stderr'), self.assertRaises(SystemExit):
                fi.parse_args(['--device', dev, '--arch', 'aarch64'])


class RootfsInputs(unittest.TestCase):
    def test_armv7_is_the_existing_set(self):
        r = fi.rootfs_inputs('armv7')
        self.assertTrue(r['minirootfs_url'].endswith('/v3.24/releases/armv7/alpine-minirootfs-3.24.2-armv7.tar.gz'))
        self.assertEqual(r['minirootfs_sha256'], '5552f1ef2398cee46ef2cf733f8f1258f39a77ad065dfd47282fc0df9fe1976c')
        self.assertEqual(r['apks312'], 'apks312')

    def test_aarch64_set(self):
        r = fi.rootfs_inputs('aarch64')
        self.assertTrue(r['minirootfs_url'].endswith('/v3.24/releases/aarch64/alpine-minirootfs-3.24.2-aarch64.tar.gz'))
        self.assertEqual(r['minirootfs_sha256'], '9bf70a7f18ea44094cbb5f70c58f9af129c8214745743db0e68e5502cc2ce773')
        self.assertEqual(r['apks312'], 'apks312-aarch64')


class MainPackageSelection(unittest.TestCase):
    """main() with every download recorded: what goes where for each architecture."""

    def run_main(self, argv):
        calls = {'apk': [], 'checked': [], 'alpine': []}

        def get_apk(filename, dest, branch='v3.24', arch='armv7'):
            calls['apk'].append((filename, os.path.basename(dest), branch, arch))
            return os.path.join(dest, filename)

        def download_checked(url, out, sha):
            calls['checked'].append((url, os.path.basename(out), sha))

        def alpine(out):
            calls['alpine'].append(out)

        out = tempfile.mkdtemp()
        self.addCleanup(lambda: __import__('shutil').rmtree(out, ignore_errors=True))
        with mock.patch.object(fi, 'get_apk', get_apk), \
                mock.patch.object(fi, 'download_checked', download_checked), \
                mock.patch.object(fi, 'alpine', alpine), \
                mock.patch.object(fi, 'from_apk', lambda *a: None), \
                mock.patch.object(fi, 'step', lambda *a: None), \
                mock.patch('builtins.print'), \
                mock.patch.object(sys, 'argv', ['fetch-inputs.py'] + argv + ['--out', out]):
            fi.main()
        return calls

    def test_show_fetches_no_aarch64_inputs(self):
        c = self.run_main(['--device', 'show'])
        self.assertEqual(len(c['alpine']), 1)
        self.assertFalse([x for x in c['apk'] if x[3] == 'aarch64'])
        self.assertFalse([x for x in c['checked'] if 'aarch64' in x[0]])
        for name in WPA_SET:
            self.assertIn((name, 'apks312', 'v3.12', 'armv7'), c['apk'])

    def test_cronos_adds_the_aarch64_rootfs_base_and_wpa_set(self):
        c = self.run_main(['--device', 'cronos'])
        # The boot layer (rescue initramfs, busybox.static) stays armv7, as on every Show.
        self.assertEqual(len(c['alpine']), 1)
        self.assertIn(('busybox-static-1.37.0-r31.apk', '.fetch', 'v3.24', 'armv7'), c['apk'])
        for name in WPA_SET:
            self.assertIn((name, 'apks312', 'v3.12', 'armv7'), c['apk'])
        # The root filesystem's base and its wpa_supplicant 2.9 set are aarch64.
        self.assertIn((fi.rootfs_inputs('aarch64')['minirootfs_url'], 'alpine-minirootfs-3.24.2-aarch64.tar.gz',
                       '9bf70a7f18ea44094cbb5f70c58f9af129c8214745743db0e68e5502cc2ce773'), c['checked'])
        for name in WPA_SET:
            self.assertIn((name, 'apks312-aarch64', 'v3.12', 'aarch64'), c['apk'])
        # Nothing else is fetched for aarch64: the initramfs packages are armv7.
        a64 = [x for x in c['apk'] if x[3] == 'aarch64']
        self.assertEqual(sorted(x[0] for x in a64), sorted(WPA_SET))


if __name__ == '__main__':
    unittest.main()
