#!/usr/bin/env python3
"""Build the pinned official Caddy on a Linux amd64 CI runner, never a server.

The archive contains caddy and LICENSE at its root. SHA256SUMS covers every
artifact file except itself, including the manifest and inspection evidence.
Only ``adapt`` is used for the supplied Caddyfile: no certificate provisioning,
server startup, external certificate files, or upstream connections are needed.
"""

from __future__ import annotations

import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys
import tarfile
import tempfile


CADDY_VERSION = "v2.11.7"
CADDY_REVISION = "72dd0fb067f6d7826c7f79907670ba4a713bfe37"
CADDY_MODULE = "github.com/caddyserver/caddy/v2"
GO_VERSION = "go1.27.3"
ARCHIVE_NAME = "caddy-linux-amd64.tar.gz"
REQUIRED_MODULES = {
    "http",
    "tls",
    "http.handlers.reverse_proxy",
    "http.handlers.headers",
    "http.handlers.request_body",
    "http.handlers.encode",
    "http.encoders.gzip",
    "http.encoders.zstd",
    "http.reverse_proxy.transport.http",
    "tls.issuance.acme",
    "caddy.logging.encoders.json",
    "caddy.logging.writers.file",
}
EXPECTED_SETTINGS = {
    "CGO_ENABLED": "0",
    "GOOS": "linux",
    "GOARCH": "amd64",
    "GOAMD64": "v1",
    "vcs": "git",
    "vcs.revision": CADDY_REVISION,
    "vcs.modified": "false",
}


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def run(args: list[str], cwd: Path, env: dict[str, str]) -> str:
    result = subprocess.run(
        args, cwd=cwd, env=env, capture_output=True, text=True,
        encoding="utf-8", errors="replace", timeout=1200,
    )
    if result.returncode:
        raise RuntimeError(
            f"{args[0]} {args[1]} failed ({result.returncode}):\n"
            f"{result.stdout[-12000:]}\n{result.stderr[-12000:]}"
        )
    return result.stdout


def build_environment() -> dict[str, str]:
    env = os.environ.copy()
    env.update({
        "CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "amd64",
        "GOAMD64": "v1", "GOTOOLCHAIN": "local", "GOWORK": "off",
        "GOENV": "off", "GOFLAGS": "", "GOEXPERIMENT": "",
    })
    return env


def verify_source(source: Path, env: dict[str, str]) -> int:
    revision = run(["git", "rev-parse", "HEAD"], source, env).strip()
    require(revision == CADDY_REVISION, f"Unexpected Caddy revision: {revision}")
    require(
        not run(["git", "status", "--porcelain", "--untracked-files=all"], source, env),
        "Official Caddy checkout must be clean",
    )
    epoch = int(run(["git", "show", "-s", "--format=%ct", "HEAD"], source, env))
    require(epoch >= 0, "Invalid source commit timestamp")
    return epoch


def verify_build_info(text: str) -> dict[str, str]:
    require(
        bool(text.splitlines()) and text.splitlines()[0].split()[-1] == GO_VERSION,
        f"Binary was not built with {GO_VERSION}",
    )
    settings = {}
    for line in text.splitlines():
        match = re.fullmatch(r"\s*build\s+(\S+?)=(.*)", line)
        if match:
            settings[match[1]] = match[2]
    for key, expected in EXPECTED_SETTINGS.items():
        require(settings.get(key) == expected, f"Unexpected build setting {key}")
    return settings


def verify_elf(binary: Path) -> None:
    """Check Linux amd64 ELF and reject a dynamic program interpreter."""
    with binary.open("rb") as stream:
        header = stream.read(64)
        require(
            len(header) == 64 and header[:7] == b"\x7fELF\x02\x01\x01",
            "Expected a little-endian ELF64 binary",
        )
        require(struct.unpack_from("<H", header, 18)[0] == 62, "Expected ELF amd64")
        offset = struct.unpack_from("<Q", header, 32)[0]
        entry_size, count = struct.unpack_from("<HH", header, 54)
        require(entry_size >= 56 and count > 0, "Missing ELF program headers")
        require(
            offset >= 64 and offset + entry_size * count <= binary.stat().st_size,
            "Invalid ELF program header bounds",
        )
        for index in range(count):
            stream.seek(offset + index * entry_size)
            require(
                struct.unpack("<I", stream.read(4))[0] != 3,
                "Static Caddy binary must not contain PT_INTERP",
            )


def verify_modules(text: str) -> None:
    modules = {line.strip() for line in text.splitlines()}
    missing = REQUIRED_MODULES - modules
    require(not missing, f"Missing standard modules: {', '.join(sorted(missing))}")


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def write_archive(stage: Path, epoch: int) -> None:
    with (stage / ARCHIVE_NAME).open("wb") as raw:
        with gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=epoch) as zipped:
            with tarfile.open(fileobj=zipped, mode="w", format=tarfile.USTAR_FORMAT) as archive:
                for name, mode in (("caddy", 0o755), ("LICENSE", 0o644)):
                    path = stage / name
                    info = tarfile.TarInfo(name)
                    info.size = path.stat().st_size
                    info.mode = mode
                    info.mtime = epoch
                    info.uid = info.gid = 0
                    info.uname = info.gname = ""
                    with path.open("rb") as content:
                        archive.addfile(info, content)


def build(source: Path, output: Path, caddyfile: Path, project_revision: str) -> dict:
    source, output, caddyfile = source.resolve(), output.resolve(), caddyfile.resolve()
    require(sys.platform == "linux", "Run executable verification on the Linux CI runner")
    require(source.is_dir(), "Caddy source directory does not exist")
    require(caddyfile.is_file(), "Caddyfile does not exist")
    require(not output.is_relative_to(source), "Output must be outside the Caddy checkout")
    require(not output.exists() or not any(output.iterdir()), "Output directory must be empty")
    require(bool(re.fullmatch(r"[0-9a-f]{40}", project_revision)), "Invalid project revision")
    env = build_environment()
    epoch = verify_source(source, env)
    goversion = run(["go", "env", "GOVERSION"], source, env).strip()
    require(goversion == GO_VERSION, f"Expected {GO_VERSION}, found {goversion}")
    package = json.loads(run(
        ["go", "list", "-mod=readonly", "-json", "./cmd/caddy"], source, env,
    ))
    require(
        package.get("Name") == "main"
        and package.get("ImportPath") == f"{CADDY_MODULE}/cmd/caddy",
        "Pinned cmd/caddy must be the official executable main package",
    )
    require(
        f"{CADDY_MODULE}/modules/standard" in package.get("Imports", []),
        "Official main package must import modules/standard",
    )
    # A checkout build has a (devel) module version. Caddy supports this linker
    # variable for its release label; the VCS revision is checked independently.
    with tempfile.TemporaryDirectory(prefix="caddy-security-") as temporary:
        stage = Path(temporary)
        binary = stage / "caddy"
        run([
            "go", "build", "-mod=readonly", "-trimpath", "-buildvcs=true",
            "-ldflags", f"-X {CADDY_MODULE}.CustomVersion={CADDY_VERSION}",
            "-o", str(binary), "./cmd/caddy",
        ], source, env)
        verify_elf(binary)
        info = run(["go", "version", "-m", str(binary)], source, env)
        settings = verify_build_info(info)
        # These commands inspect/parse only; do not use --validate, run or start.
        offline_env = dict(env, GOPROXY="off", GOSUMDB="off")
        version = run([str(binary), "version"], source, offline_env).strip()
        require(
            version.split() and version.split()[0] == CADDY_VERSION,
            f"Unexpected Caddy release label: {version}",
        )
        modules = run([str(binary), "list-modules"], source, offline_env)
        verify_modules(modules)
        runtime_info = run([str(binary), "build-info"], source, offline_env)
        # Relative imports must resolve beside the supplied configuration.
        config_hash = sha256(caddyfile)
        adapted = run([
            str(binary), "adapt", "--config", str(caddyfile),
            "--adapter", "caddyfile", "--pretty",
        ], caddyfile.parent, offline_env)
        require(isinstance(json.loads(adapted), dict), "Adaptation must return a JSON object")
        require(sha256(caddyfile) == config_hash, "Caddyfile changed during adaptation")
        verify_source(source, env)
        evidence = {
            "build-info.txt": info,
            "caddy-build-info.txt": runtime_info,
            "version.txt": version + "\n",
            "modules.txt": modules,
            "adapted.json": adapted,
        }
        for name, text in evidence.items():
            (stage / name).write_text(text, encoding="utf-8", newline="\n")
        shutil.copyfile(source / "LICENSE", stage / "LICENSE")
        binary.chmod(0o755)
        write_archive(stage, epoch)
        manifest = {
            "schema_version": 1,
            "project_revision": project_revision,
            "caddy": {
                "repository": "https://github.com/caddyserver/caddy",
                "version": CADDY_VERSION,
                "revision": CADDY_REVISION,
                "reported_version": version,
            },
            "go_version": goversion,
            "build_settings": settings,
            "source_date_epoch": epoch,
            "caddyfile": {"name": caddyfile.name, "sha256": config_hash},
            "checks": {
                "clean_pinned_source": True,
                "static_linux_amd64_elf": True,
                "official_main_imports_standard": True,
                "required_modules": sorted(REQUIRED_MODULES),
                "offline_adapt": True,
            },
            "files": {
                path.name: {"sha256": sha256(path), "size": path.stat().st_size}
                for path in sorted(stage.iterdir())
            },
        }
        (stage / "manifest.json").write_text(
            json.dumps(manifest, indent=2, sort_keys=True) + "\n",
            encoding="utf-8", newline="\n",
        )
        checksums = "".join(
            f"{sha256(path)}  {path.name}\n" for path in sorted(stage.iterdir())
        )
        (stage / "SHA256SUMS").write_text(checksums, encoding="ascii", newline="\n")
        # Publish locally only after every build/inspection/adaptation gate passes.
        output.parent.mkdir(parents=True, exist_ok=True)
        shutil.copytree(stage, output, dirs_exist_ok=True)
    return manifest


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-dir", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, default=Path("dist/caddy-security"))
    parser.add_argument("--caddyfile", type=Path, default=Path("deploy/Caddyfile"))
    parser.add_argument("--project-revision", default=os.environ.get("GITHUB_SHA", ""))
    args = parser.parse_args()
    try:
        manifest = build(args.source_dir, args.output_dir, args.caddyfile, args.project_revision)
    except (ValueError, RuntimeError, OSError, subprocess.SubprocessError) as error:
        print(f"Caddy security build failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(manifest, indent=2, sort_keys=True))
    print(f"Verified artifact: {args.output_dir.resolve()}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
