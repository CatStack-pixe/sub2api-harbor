"""Offline regression tests for provenance rejection and the deployment bundle.

Compiler/process boundaries are simulated here. The workflow separately builds
and executes the real pinned Linux binary, including Caddyfile adaptation.
"""

from contextlib import ExitStack
import hashlib
import json
from pathlib import Path
import struct
import tempfile
import unittest
from unittest.mock import patch

import build_caddy as builder


def elf_bytes(machine=62, program_type=1):
    data = bytearray(120)
    data[:7] = b"\x7fELF\x02\x01\x01"
    struct.pack_into("<H", data, 18, machine)
    struct.pack_into("<Q", data, 32, 64)
    struct.pack_into("<HH", data, 54, 56, 1)
    struct.pack_into("<I", data, 64, program_type)
    return bytes(data)


def build_info(**overrides):
    settings = dict(builder.EXPECTED_SETTINGS, **overrides)
    return "caddy: go1.27.3\n" + "".join(
        f"\tbuild\t{key}={value}\n" for key, value in settings.items()
    )


class BuildBoundary:
    """Fake external tools while exercising actual checksums, ELF and tar I/O."""

    def __init__(self):
        self.calls = []
        self.go_version = builder.GO_VERSION
        self.revision = builder.CADDY_REVISION
        self.dirty = ""
        self.main_name = "main"
        self.standard = True
        self.info = build_info()
        self.modules = set(builder.REQUIRED_MODULES)
        self.version = f"{builder.CADDY_VERSION} {builder.CADDY_REVISION}"
        self.adapt_error = False
        self.adapted = '{"apps":{"http":{"servers":{}}}}\n'

    def __call__(self, args, cwd, env):
        self.calls.append((args, cwd, env))
        if args[0] == "git":
            if args[1] == "rev-parse":
                return self.revision + "\n"
            if args[1] == "status":
                return self.dirty
            if args[1] == "show":
                return "1792627200\n"
        if args[:3] == ["go", "env", "GOVERSION"]:
            return self.go_version
        if args[:2] == ["go", "list"]:
            return json.dumps({
                "Name": self.main_name,
                "ImportPath": f"{builder.CADDY_MODULE}/cmd/caddy",
                "Imports": [f"{builder.CADDY_MODULE}/modules/standard"] if self.standard else [],
            })
        if args[:2] == ["go", "build"]:
            if "-buildvcs=true" not in args or "-mod=readonly" not in args:
                raise AssertionError("Build must preserve provenance and the pinned module graph")
            if args[-1] != "./cmd/caddy":
                raise AssertionError("Build must use the official entry point")
            if f"-X {builder.CADDY_MODULE}.CustomVersion={builder.CADDY_VERSION}" not in args:
                raise AssertionError("Caddy release label must be set")
            Path(args[args.index("-o") + 1]).write_bytes(elf_bytes())
            return ""
        if args[:3] == ["go", "version", "-m"]:
            return self.info
        if args[1] == "version":
            return self.version + "\n"
        if args[1] == "build-info":
            return "go\tgo1.27.3\n"
        if args[1] == "list-modules":
            return "\n".join(sorted(self.modules)) + "\n"
        if args[1] == "adapt":
            if self.adapt_error:
                raise RuntimeError("adapt failed: invalid direct block")
            return self.adapted
        raise AssertionError(f"Unexpected tool invocation: {args}")


class BuildTests(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        temporary = self.stack.enter_context(tempfile.TemporaryDirectory())
        self.root = Path(temporary)
        self.source = self.root / "official caddy"
        self.source.mkdir()
        (self.source / "LICENSE").write_text("Apache License\n", encoding="utf-8")
        self.config = self.root / "direct block.Caddyfile"
        self.config.write_text(":8080 {\n respond ok\n}\n", encoding="utf-8")
        self.output = self.root / "artifacts"
        self.boundary = BuildBoundary()
        self.stack.enter_context(patch.object(builder, "run", self.boundary))
        self.stack.enter_context(patch.object(builder.sys, "platform", "linux"))

    def build(self):
        return builder.build(self.source, self.output, self.config, "a" * 40)

    def test_complete_artifact_has_verified_archive_and_all_file_hashes(self):
        manifest = self.build()
        expected = {
            "caddy", "LICENSE", builder.ARCHIVE_NAME, "manifest.json", "SHA256SUMS",
            "build-info.txt", "caddy-build-info.txt", "version.txt", "modules.txt",
            "adapted.json",
        }
        self.assertEqual({p.name for p in self.output.iterdir()}, expected)
        self.assertEqual(manifest, json.loads((self.output / "manifest.json").read_text()))
        self.assertEqual(manifest["caddy"]["revision"], builder.CADDY_REVISION)
        self.assertEqual(manifest["go_version"], builder.GO_VERSION)
        self.assertEqual(manifest["caddyfile"]["sha256"], builder.sha256(self.config))
        checksums = {}
        for line in (self.output / "SHA256SUMS").read_text().splitlines():
            digest, name = line.split("  ", 1)
            checksums[name] = digest
            self.assertEqual(digest, hashlib.sha256((self.output / name).read_bytes()).hexdigest())
        self.assertEqual(set(checksums), expected - {"SHA256SUMS"})
        for name, details in manifest["files"].items():
            self.assertEqual(details["sha256"], checksums[name])
            self.assertEqual(details["size"], (self.output / name).stat().st_size)
        with builder.tarfile.open(self.output / builder.ARCHIVE_NAME, "r:gz") as archive:
            self.assertEqual(archive.getnames(), ["caddy", "LICENSE"])
            member = archive.getmember("caddy")
            self.assertEqual(member.mode, 0o755)
            self.assertEqual(member.uid, 0)
            self.assertEqual(member.mtime, manifest["source_date_epoch"])
            self.assertEqual(archive.extractfile(member).read(), (self.output / "caddy").read_bytes())

    def test_offline_adaptation_resolves_relative_imports_next_to_config(self):
        self.build()
        args, cwd, env = next(c for c in self.boundary.calls if c[0][1] == "adapt")
        self.assertEqual(cwd, self.config.parent)
        self.assertEqual(args[args.index("--config") + 1], str(self.config))
        self.assertNotIn("--validate", args)
        self.assertEqual(env["GOPROXY"], "off")
        self.assertEqual(env["GOSUMDB"], "off")

    def test_archive_is_reproducible(self):
        self.build()
        original = (self.output / builder.ARCHIVE_NAME).read_bytes()
        builder.write_archive(self.output, 1792627200)
        self.assertEqual((self.output / builder.ARCHIVE_NAME).read_bytes(), original)

    def test_wrong_go_version_stops_before_compilation(self):
        self.boundary.go_version = "go1.27.2"
        with self.assertRaisesRegex(ValueError, "Expected go1.27.3"):
            self.build()
        self.assertFalse(any(c[0][:2] == ["go", "build"] for c in self.boundary.calls))
        self.assertFalse(self.output.exists())

    def test_wrong_source_revision_is_rejected(self):
        self.boundary.revision = "b" * 40
        with self.assertRaisesRegex(ValueError, "Unexpected Caddy revision"):
            self.build()
        self.assertFalse(self.output.exists())

    def test_dirty_source_is_rejected(self):
        self.boundary.dirty = " M go.mod\n"
        with self.assertRaisesRegex(ValueError, "checkout must be clean"):
            self.build()

    def test_library_package_is_rejected(self):
        self.boundary.main_name = "caddycmd"
        with self.assertRaisesRegex(ValueError, "executable main"):
            self.build()

    def test_missing_standard_import_is_rejected(self):
        self.boundary.standard = False
        with self.assertRaisesRegex(ValueError, "import modules/standard"):
            self.build()

    def test_wrong_binary_release_label_is_rejected(self):
        self.boundary.version = "v2.11.4"
        with self.assertRaisesRegex(ValueError, "release label"):
            self.build()
        self.assertFalse(self.output.exists())

    def test_missing_runtime_module_is_rejected(self):
        self.boundary.modules.remove("tls")
        with self.assertRaisesRegex(ValueError, "Missing standard modules: tls"):
            self.build()

    def test_failed_adaptation_leaves_no_deployment_artifact(self):
        self.boundary.adapt_error = True
        with self.assertRaisesRegex(RuntimeError, "invalid direct block"):
            self.build()
        self.assertFalse(self.output.exists())

    def test_invalid_adapted_json_leaves_no_deployment_artifact(self):
        self.boundary.adapted = "[]"
        with self.assertRaisesRegex(ValueError, "JSON object"):
            self.build()
        self.assertFalse(self.output.exists())

    def test_stale_output_is_rejected_without_overwriting(self):
        self.output.mkdir()
        stale = self.output / "manifest.json"
        stale.write_text("stale", encoding="ascii")
        with self.assertRaisesRegex(ValueError, "must be empty"):
            self.build()
        self.assertEqual(stale.read_text(), "stale")

    def test_output_inside_checkout_is_rejected(self):
        self.output = self.source / "artifacts"
        with self.assertRaisesRegex(ValueError, "outside the Caddy checkout"):
            self.build()

    def test_toolchain_environment_overrides_host_settings(self):
        with patch.dict(builder.os.environ, {"GOTOOLCHAIN": "auto", "CGO_ENABLED": "1",
                                             "GOFLAGS": "-mod=mod", "GOWORK": "some.work"}):
            env = builder.build_environment()
        self.assertEqual(env["GOTOOLCHAIN"], "local")
        self.assertEqual(env["CGO_ENABLED"], "0")
        self.assertEqual(env["GOWORK"], "off")
        self.assertEqual(env["GOFLAGS"], "")


class VerificationTests(unittest.TestCase):
    def test_wrong_embedded_provenance_or_target_is_rejected(self):
        for key, value in {
            "CGO_ENABLED": "1", "GOOS": "windows", "GOARCH": "arm64", "GOAMD64": "v3",
            "vcs": "hg", "vcs.revision": "b" * 40, "vcs.modified": "true",
        }.items():
            with self.subTest(key=key):
                with self.assertRaisesRegex(ValueError, key):
                    builder.verify_build_info(build_info(**{key: value}))
        with self.assertRaisesRegex(ValueError, "go1.27.3"):
            builder.verify_build_info(build_info().replace("go1.27.3", "go1.26.3"))

    def test_missing_embedded_revision_is_rejected(self):
        info = build_info().replace(f"\tbuild\tvcs.revision={builder.CADDY_REVISION}\n", "")
        with self.assertRaisesRegex(ValueError, "vcs.revision"):
            builder.verify_build_info(info)

    def test_wrong_architecture_dynamic_linker_and_truncated_elf_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / "caddy"
            for content, message in [
                (elf_bytes(machine=183), "ELF amd64"),
                (elf_bytes(program_type=3), "PT_INTERP"),
                (elf_bytes()[:70], "header bounds"),
                (b"MZ" + bytes(120), "ELF64"),
            ]:
                with self.subTest(message=message):
                    binary.write_bytes(content)
                    with self.assertRaisesRegex(ValueError, message):
                        builder.verify_elf(binary)


if __name__ == "__main__":
    unittest.main()
