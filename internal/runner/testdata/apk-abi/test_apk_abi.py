import json
import os
from pathlib import Path
import re
import runpy
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
from concurrent.futures import ThreadPoolExecutor


ASSETS = Path(__file__).resolve().parents[2] / "assets" / "apk-abi"
FIXTURES = Path(__file__).resolve().parent


class APKABIRegression(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.top = Path(self.tmp.name)
        self.patch = self.top / ".asu/patches/apk-abi"
        shutil.copytree(ASSETS, self.patch)
        self.env = dict(os.environ, CALL_LOG=str(self.top / "calls.jsonl"))
        self.bin = self.top / "bin"
        self.bin.mkdir()
        self.write("bin/fakeroot", '#!/bin/sh\nprintf "fakeroot\\n" >> "$CALL_LOG"\nexec "$@"\n', executable=True)
        self.write("bin/apk", '''#!/usr/bin/env python3
import json, os, sys
with open(os.environ['CALL_LOG'], 'a') as f:
    f.write(json.dumps({'argv': sys.argv[1:], 'root': os.getenv('IPKG_INSTROOT')}) + '\\n')
if 'query' in sys.argv:
    package = sys.argv[-1]
    if package == 'failure':
        sys.stderr.write('query failure\\n')
        sys.exit(3)
    if package != 'negative':
        sys.stdout.buffer.write(b'\\x00\\xffraw\\n' if package == 'raw' else b'openwrt:abiversion=42\\n')
''', executable=True)
        self.write("root/lib/apk/db/installed", "installed-v1")
        self.write("keys/key.pub", "key-v1")
        self.write("dl/packages.adb", "index-v1")
        self.write("packages/packages.adb", "local-v1")
        self.write("repositories", "https://example.invalid/packages.adb\npackages/packages.adb\n")

    def write(self, name, content, executable=False):
        path = self.top / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        if executable:
            path.chmod(0o755)
        return path

    def run_process(self, args, check=True):
        return subprocess.run(args, cwd=self.top, env=self.env, stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE, check=check)

    def calls(self):
        path = self.top / "calls.jsonl"
        return path.read_text().splitlines() if path.exists() else []

    def query(self, package="positive", extra=(), check=True):
        return self.run_process([sys.executable, str(self.patch / "query.py"), str(self.bin / "apk"),
                                 "--root", str(self.top / "root"), "--keys-dir", str(self.top / "keys"),
                                 "--repositories-file", str(self.top / "repositories"),
                                 "--repository", str(self.top / "packages/packages.adb"),
                                 "--no-logfile", "--preserve-env", *extra,
                                 "query", "--fields", "tags", "--match", "provides", package], check)

    def test_positive_and_negative_cache(self):
        for package in ("positive", "negative"):
            first = self.query(package)
            second = self.query(package)
            self.assertEqual(first.stdout, second.stdout)
        self.assertEqual(len(self.calls()), 2)

    def test_raw_stdout_bytes_are_preserved(self):
        for _ in range(2):
            self.assertEqual(self.query("raw").stdout, b"\x00\xffraw\n")
        self.assertEqual(len(self.calls()), 1)

    def test_relevant_environment_invalidates_cache(self):
        self.query()
        for name in ("IPKG_INSTROOT", "APK_TEST_CONTEXT", "LANG", "HOME"):
            self.env[name] = "changed-context"
            self.query()
        self.assertEqual(len(self.calls()), 5)

    def test_failures_not_cached(self):
        for _ in range(2):
            self.assertEqual(self.query("failure", check=False).returncode, 3)
        self.assertEqual(len(self.calls()), 2)

    def test_context_changes(self):
        self.query()
        for path in ("dl/packages.adb", "dl/APKINDEX.tar.gz", "root/lib/apk/db/installed", "keys/key.pub",
                     "repositories", "packages/packages.adb"):
            with (self.top / path).open("a") as f:
                f.write("\nchanged")
            self.query()
        self.assertEqual(len(self.calls()), 7)

    def test_argument_boundaries(self):
        value = "semi;$(touch SHOULD_NOT_EXIST) space ' quote"
        self.query(value)
        call = json.loads(self.calls()[0])
        self.assertEqual(call["argv"][-1], value)
        self.assertFalse((self.top / "SHOULD_NOT_EXIST").exists())

    def test_task_isolation(self):
        self.query()
        other = self.top / "another/.asu/patches/apk-abi"
        shutil.copytree(ASSETS, other)
        original = self.patch
        self.patch = other
        try:
            self.query()
        finally:
            self.patch = original
        self.assertEqual(len(self.calls()), 2)

    def test_concurrent_queries(self):
        with ThreadPoolExecutor(max_workers=6) as pool:
            results = list(pool.map(lambda _: self.query(), range(6)))
        self.assertEqual(len(self.calls()), 1)
        self.assertTrue(all(result.stdout == b"openwrt:abiversion=42\n" for result in results))

    def test_unknown_context_runs_uncached(self):
        for _ in range(2):
            result = self.query(extra=("--unknown-option",))
            self.assertIn("直接查询".encode(), result.stderr)
        self.assertEqual(len(self.calls()), 2)

    def test_force_refresh_runs_uncached(self):
        for _ in range(2):
            result = self.query(extra=("--force-refresh",))
            self.assertIn("直接查询".encode(), result.stderr)
        self.assertEqual(len(self.calls()), 2)

    def test_cache_io_failure_runs_uncached(self):
        (self.patch / "cache").write_text("not a directory")
        for _ in range(2):
            self.assertIn("直接查询".encode(), self.query().stderr)
        self.assertEqual(len(self.calls()), 2)

    def test_corrupt_cache_is_not_an_empty_hit(self):
        self.query()
        entry = next((self.patch / "cache").glob("*.json"))
        entry.write_text('{"stdout": "not base64"}')
        result = self.query()
        self.assertEqual(result.stdout, b"openwrt:abiversion=42\n")
        self.assertIn("缓存读取失败".encode(), result.stderr)
        self.assertEqual(len(self.calls()), 2)

    def source_fixture(self):
        source = FIXTURES / "immortalwrt-25.12.2"
        for name in ("Makefile", "include/rootfs.mk", "rules.mk"):
            target = self.top / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source / name, target)

    def apply(self, check=True):
        return self.run_process([sys.executable, str(self.patch / "apply.py")], check)

    def make_fixture(self):
        makefile = (self.top / "Makefile").read_text()
        rules = (self.top / "rules.mk").read_text()
        fakeroot = re.search(r"^FAKEROOT:=.*$", rules, re.M).group()
        apk = re.search(r"^export APK_KEYS:=.*\nAPK:=(?:[^\n]*\\\n)+[^\n]*", makefile, re.M).group()
        abi = re.search(r"^define GetABISuffix\n.*?\nendef", makefile, re.M | re.S).group()
        self.write("abi-functions.mk", fakeroot + "\n" + apk + "\n" + abi + "\n")
        shutil.copyfile(FIXTURES / "fixture.mk", self.top / "fixture.mk")

    def test_make_reuses_queries_and_preserves_install(self):
        self.source_fixture()
        if not os.environ.get("APK_ABI_TEST_UNPATCHED"):
            self.assertIn("已应用：".encode(), self.apply().stderr)
        self.make_fixture()
        first = self.run_process(["make", "--no-print-directory", "-s", "-f", "fixture.mk", "STRIP_ABI=1", "manifest"])
        second = self.run_process(["make", "--no-print-directory", "-s", "-f", "fixture.mk",
                                   "BIN_DIR=output-images", "EXTRA_IMAGE_NAME=custom-name", "V=s", "image"])
        self.assertEqual(first.stdout, b"42\n\n")
        self.assertEqual(second.stdout, first.stdout)
        calls = self.calls()
        queries = [json.loads(line) for line in calls if line.startswith("{")]
        self.assertEqual(len(queries), 2, "ABI正负查询应跨manifest/image各只执行一次")
        self.assertNotIn("fakeroot", calls, "ABI查询不应经过fakeroot")
        for query in queries:
            self.assertEqual(query["root"], str(self.top / "root"))
            self.assertEqual(query["argv"][:13], ["--root", str(self.top / "root"),
                "--keys-dir", str(self.top / "keys"), "--no-logfile", "--preserve-env",
                "--repositories-file", str(self.top / "repositories"), "--repository",
                str(self.top / "packages/packages.adb"), "--allow-untrusted", "--cache-dir", str(self.top / "dl")])
        self.run_process(["make", "--no-print-directory", "-s", "-f", "fixture.mk", "install", "installed"])
        self.assertEqual(self.calls().count("fakeroot"), 2, "安装和installed manifest查询必须保留fakeroot")

    def test_guard_unknown_duplicate_and_idempotent(self):
        self.source_fixture()
        original = (self.top / "Makefile").read_bytes()
        for name in ("Makefile", "include/rootfs.mk", "rules.mk"):
            path = self.top / name
            before = path.read_bytes()
            path.write_bytes(before + b"\n# unknown source\n")
            result = self.apply()
            self.assertIn("警告".encode(), result.stderr)
            self.assertEqual((self.top / "Makefile").read_bytes(), before + b"\n# unknown source\n" if name == "Makefile" else original)
            path.write_bytes(before)
        (self.top / "Makefile").write_bytes(original + b"\ndefine GetABISuffix\nunknown\nendef\n")
        before = (self.top / "Makefile").read_bytes()
        self.assertIn("警告".encode(), self.apply().stderr)
        self.assertEqual((self.top / "Makefile").read_bytes(), before)
        (self.top / "Makefile").write_bytes(original)
        self.assertIn("已应用：".encode(), self.apply().stderr)
        patched = (self.top / "Makefile").read_bytes()
        self.assertIn("已应用，无需重复".encode(), self.apply().stderr)
        self.assertEqual((self.top / "Makefile").read_bytes(), patched)

    def test_empty_fakeroot_safely_falls_back(self):
        self.source_fixture()
        self.apply()
        self.make_fixture()
        for target in ("manifest", "image"):
            self.run_process(["make", "--no-print-directory", "-s", "-f", "fixture.mk", "FAKEROOT=", target])
        self.assertEqual(len(self.calls()), 4)
        self.assertFalse((self.patch / "cache").exists())

    def test_atomic_apply_failure_keeps_original(self):
        self.source_fixture()
        original = (self.top / "Makefile").read_bytes()
        module = runpy.run_path(str(self.patch / "apply.py"))
        cwd = Path.cwd()
        try:
            os.chdir(self.top)
            with mock.patch("os.replace", side_effect=PermissionError("injected replace failure")):
                with self.assertRaises(PermissionError):
                    module["apply"]()
        finally:
            os.chdir(cwd)
        self.assertEqual((self.top / "Makefile").read_bytes(), original)
        self.assertEqual(list(self.top.glob(".asu-apk-abi-*")), [])


if __name__ == "__main__":
    unittest.main()
