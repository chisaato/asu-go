"""Atomically patch only a fully recognized ImageBuilder source snapshot."""

import hashlib
import json
import os
from pathlib import Path
import stat
import sys
import tempfile


def log(message):
    print("[asu apk-abi] " + message, file=sys.stderr)


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def apply():
    assets = Path(__file__).resolve().parent
    guards = json.loads((assets / "guard.json").read_text())
    replacement = (assets / "GetABISuffix.mk").read_bytes().rstrip(b"\n")
    top = Path.cwd()
    for guard in guards:
        inputs = {}
        try:
            for name in guard["files"]:
                path = top / name
                if path.is_symlink() or not path.is_file():
                    break
                inputs[name] = path.read_bytes()
        except FileNotFoundError:
            continue
        if len(inputs) != len(guard["files"]):
            continue
        original = guard["original"].encode()
        makefile = inputs["Makefile"]
        # Idempotency is recognized only with the same exact surrounding
        # source snapshot, not by trusting a marker comment or loose regex.
        already = makefile.count(replacement) == 1
        if already and sha256(makefile) != guard["patched_sha256"]:
            continue
        candidate = makefile.replace(replacement, original, 1) if already else makefile
        if any(sha256(candidate if name == "Makefile" else inputs[name]) != fingerprint
               for name, fingerprint in guard["files"].items()):
            continue
        if candidate.count(original) != 1:
            continue
        if already:
            log("已应用，无需重复：" + guard["name"])
            return 0
        updated = candidate.replace(original, replacement, 1)
        if sha256(updated) != guard["patched_sha256"]:
            raise RuntimeError("替换资源与预期补丁指纹不匹配")
        target = top / "Makefile"
        temporary = None
        try:
            mode = stat.S_IMODE(target.stat().st_mode)
            with tempfile.NamedTemporaryFile(dir=top, prefix=".asu-apk-abi-", delete=False) as stream:
                temporary = stream.name
                os.fchmod(stream.fileno(), mode)
                stream.write(updated)
                stream.flush()
                os.fsync(stream.fileno())
            # Reject concurrent edits rather than overwrite them silently.
            if target.read_bytes() != makefile:
                raise RuntimeError("应用补丁期间 Makefile 被修改")
            os.replace(temporary, target)
            temporary = None
        finally:
            if temporary is not None:
                os.unlink(temporary)
        log("已应用：" + guard["name"])
        return 0
    log("警告：ImageBuilder 源码不匹配，已跳过 ABI 优化（未修改文件）")
    return 0


if __name__ == "__main__":
    # Unexpected errors on recognized sources intentionally remain fatal.
    sys.exit(apply())
