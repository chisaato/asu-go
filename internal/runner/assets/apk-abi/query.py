"""Task-local, conservative cache for the guarded read-only APK ABI query."""

import base64
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


# GNU make transport state and image-output formatting do not affect APK's
# read-only tags query. Manifest/image deliberately use different values here.
# Keep every other variable (including IPKG_INSTROOT, APK_*, locale, loader,
# HOME and PATH); an allowlist would risk omitting an APK dependency.
IRRELEVANT_ENV = {
    "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "MAKEOVERRIDES",
    "STRIP_ABI", "BIN_DIR", "EXTRA_IMAGE_NAME", "V",
}


def warn(message):
    print("[asu apk-abi] " + message, file=sys.stderr)


def cache_key(argv):
    # Unknown command shapes remain executable, but are never cached.
    query = argv.index("query")
    if argv[query + 1:-1] != ["--fields", "tags", "--match", "provides"]:
        raise ValueError("查询形态未识别")
    options = {}
    i = 1
    flags = {"--no-logfile", "--preserve-env", "--allow-untrusted"}
    valued = {"--root", "--keys-dir", "--repositories-file", "--repository", "--cache-dir", "--arch"}
    while i < query:
        arg = argv[i]
        if arg in valued:
            if i + 1 >= query:
                raise ValueError("选项缺少参数")
            options.setdefault(arg, []).append(argv[i + 1])
            i += 2
        elif arg in flags:
            i += 1
        else:
            raise ValueError("选项未识别：" + arg)
    for name in ("--root", "--keys-dir"):
        if len(options.get(name, [])) != 1:
            raise ValueError("选项缺失或不唯一：" + name)
    digest = hashlib.sha256()

    def record(value):
        data = value if isinstance(value, bytes) else str(value).encode()
        digest.update(str(len(data)).encode() + b":" + data)

    def file(path):
        path = Path(path).absolute()
        record(path)
        record(path.read_bytes())

    def tree(path, required=False):
        path = Path(path)
        if not path.exists():
            if required:
                raise ValueError("上下文缺失：" + str(path))
            record("missing:" + str(path.absolute()))
            return
        if not path.is_dir():
            raise ValueError("上下文路径不是目录")
        record("tree:" + str(path.absolute()))
        for child in sorted(path.rglob("*")):
            if child.is_file():
                file(child)
            elif not child.is_dir():
                raise ValueError("上下文文件类型未识别")

    record("asu-apk-abi-v1")
    record(json.dumps(argv, ensure_ascii=True))
    # APK --preserve-env permits environment-sensitive behavior. Exclude only
    # the explicitly documented make/output-only variables above.
    environment = {name: value for name, value in os.environ.items() if name not in IRRELEVANT_ENV}
    record(json.dumps(environment, sort_keys=True, ensure_ascii=True))
    record(Path.cwd())
    executable = shutil.which(argv[0])
    if executable is None:
        raise ValueError("APK 可执行文件缺失")
    file(executable)
    root = Path(options["--root"][0])
    if not root.is_dir():
        raise ValueError("根目录缺失")
    tree(root / "lib/apk/db")
    tree(root / "usr/lib/apk/db")
    tree(root / "etc/apk")
    tree(options["--keys-dir"][0], required=True)

    repositories = list(options.get("--repository", []))
    configs = options.get("--repositories-file", [str(root / "etc/apk/repositories")])
    for config in configs:
        file(config)
        for line in Path(config).read_text().splitlines():
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            # Tagged/variable/conditional repository syntaxes are deliberately
            # unsupported until their dependencies can be fingerprinted.
            if any(char.isspace() for char in line) or "$" in line:
                raise ValueError("仓库配置语法未识别")
            repositories.append(line)

    # Downloaded indexes are local snapshots of remote repository state.
    indexes = []
    for directory in [Path("dl")] + [Path(p) for p in options.get("--cache-dir", [])]:
        if directory.exists():
            for path in sorted(directory.rglob("*")):
                if path.is_file() and ("APKINDEX" in path.name or path.name.endswith(".adb")):
                    file(path)
                    indexes.append(path)
        else:
            record("missing:" + str(directory.absolute()))
    for repository in repositories:
        if repository.startswith(("https://", "http://")):
            if not indexes:
                raise ValueError("远程仓库缺少本地索引快照")
            continue
        if "://" in repository:
            raise ValueError("仓库协议未识别")
        path = Path(repository)
        if path.is_dir():
            candidates = [path / "packages.adb", path / "APKINDEX.tar.gz"]
            found = [p for p in candidates if p.is_file()]
            if not found:
                raise ValueError("本地仓库缺少索引")
            for candidate in found:
                file(candidate)
        else:
            file(path)
    return digest.hexdigest()


def run(argv):
    result = subprocess.run(argv, stdout=subprocess.PIPE)
    return result.returncode, result.stdout


def cached_run(argv):
    try:
        key = cache_key(argv)
        cache = Path(__file__).resolve().parent / "cache"
        cache.mkdir(mode=0o700, exist_ok=True)
        lock = (cache / (key + ".lock")).open("a+b")
    except (OSError, ValueError) as error:
        warn("无法安全使用缓存，直接查询：" + str(error))
        return run(argv)
    try:
        with lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            entry = cache / (key + ".json")
            try:
                content = json.loads(entry.read_text())
                if content["key"] != key:
                    raise ValueError("缓存键不匹配")
                output = base64.b64decode(content["stdout"], validate=True)
                return 0, output
            except FileNotFoundError:
                pass
            except (OSError, ValueError, KeyError, TypeError) as error:
                warn("缓存读取失败，直接查询：" + str(error))
            code, output = run(argv)
            if code == 0:
                temporary = None
                try:
                    # Do not save a result if local inputs changed in flight.
                    if cache_key(argv) != key:
                        warn("查询上下文已变化，结果不写入缓存")
                        return code, output
                    with tempfile.NamedTemporaryFile(mode="w", dir=cache, delete=False) as stream:
                        temporary = stream.name
                        json.dump({"key": key, "stdout": base64.b64encode(output).decode("ascii")}, stream)
                        stream.flush()
                        os.fsync(stream.fileno())
                    os.replace(temporary, entry)
                    temporary = None
                except (OSError, ValueError) as error:
                    warn("缓存写入失败：" + str(error))
                finally:
                    if temporary is not None:
                        try:
                            os.unlink(temporary)
                        except OSError:
                            pass
            return code, output
    except OSError as error:
        warn("缓存锁失败，直接查询：" + str(error))
        return run(argv)


def main():
    if len(sys.argv) < 2:
        warn("缺少 APK 命令")
        return 2
    code, output = cached_run(sys.argv[1:])
    sys.stdout.buffer.write(output)
    return code if code >= 0 else 128 - code


if __name__ == "__main__":
    sys.exit(main())
