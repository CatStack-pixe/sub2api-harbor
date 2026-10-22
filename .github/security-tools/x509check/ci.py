"""Run in Linux CI with an explicitly selected, preinstalled Go toolchain."""

import argparse
import json
import os
from pathlib import Path
import platform
import struct
import subprocess
import sys


DIRECTORY = Path(__file__).resolve().parent


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def assert_static_linux_amd64(binary):
    data = binary.read_bytes()
    require(data[:6] == b"\x7fELF\x02\x01", "expected a little-endian ELF64 executable")
    require(struct.unpack_from("<H", data, 18)[0] == 62, "expected Linux amd64")
    offset = struct.unpack_from("<Q", data, 32)[0]
    size, count = struct.unpack_from("<HH", data, 54)
    require(size >= 56 and offset + count * size <= len(data), "invalid ELF program headers")
    for index in range(count):
        segment = struct.unpack_from("<I", data, offset + index * size)[0]
        require(segment not in (2, 3), "binary has PT_DYNAMIC or PT_INTERP; expected static Go build")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--expect", choices=("patched", "vulnerable"), default="patched")
    parser.add_argument("--output", required=True, type=Path, help="Linux amd64 static executable path")
    args = parser.parse_args()
    require(sys.platform == "linux", "run all Go execution in Linux CI")
    require(platform.machine().lower() in ("x86_64", "amd64"), "use a Linux amd64 CI runner")
    environment = os.environ.copy()
    environment.update(
        CGO_ENABLED="0", GOOS="linux", GOARCH="amd64", GOAMD64="v1",
        GOTOOLCHAIN="local", GOWORK="off", GOPROXY="off", GOSUMDB="off", GOFLAGS="",
    )

    def go(*command):
        subprocess.run(["go", *command], cwd=DIRECTORY, env=environment, check=True)

    go("vet", "./...")
    if args.expect == "patched":
        go("test", "-count=1", "./...")
    else:
        # The security regression itself must fail on the old toolchain.
        test = subprocess.run(
            ["go", "test", "-count=1", "-run", "^TestPatchedCryptoX509RejectsDistrustedSPKIs$", "./..."],
            cwd=DIRECTORY, env=environment, text=True, capture_output=True,
        )
        print(test.stdout, end="")
        print(test.stderr, end="", file=sys.stderr)
        require(test.returncode == 1, "old-toolchain regression must fail as a test, not a build error")
        require("crypto/x509 patch missing" in test.stdout, "regression failed for an unexpected reason")
        go("test", "-count=1", "-run", "^Test(SystemRoot|FixtureTampering|CLIRejects)", "./...")
    output = args.output.resolve()
    require(DIRECTORY != output and DIRECTORY not in output.parents, "write the executable outside the source directory")
    output.parent.mkdir(parents=True, exist_ok=True)
    go("build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w", "-o", str(output), ".")
    assert_static_linux_amd64(output)
    execution = subprocess.run(
        [str(output), "-check", "crypto", "-json"], cwd=DIRECTORY,
        env=environment, text=True, capture_output=True,
    )
    print(execution.stdout, end="")
    print(execution.stderr, end="", file=sys.stderr)
    report = json.loads(execution.stdout)
    require(report["covered_spkis"] > 0 and report["official_spkis"] == 8, "missing SPKI coverage")
    require(report["exit_code"] == execution.returncode, "report/exit status mismatch")
    controls = [row for row in report["results"] if not row["name"].startswith("probe_")]
    probes = [row for row in report["results"] if row["name"].startswith("probe_")]
    require(len(controls) == 3, "fixture integrity, root and chain controls must all be present")
    require(all(row["status"] == "pass" for row in controls), "normal controls failed")
    require(len(probes) == report["covered_spkis"], "probe count mismatch")
    if args.expect == "patched":
        require(execution.returncode == 0, "patched crypto check did not pass")
        require(all(row["status"] == "pass" and "NoValidChains" in row["detail"] for row in probes),
                "patched runtime must reject every probe with NoValidChains")
    else:
        require(execution.returncode == 1, "old runtime must fail the security gate")
        require(all(row["status"] == "fail" and "accepted" in row["detail"] for row in probes),
                "old runtime must accept every otherwise valid probe")
    print(f"Verified {args.expect} behavior and static ELF: {output}")


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, ValueError, subprocess.CalledProcessError) as error:
        print(f"ci.py: {error}", file=sys.stderr)
        sys.exit(2)
