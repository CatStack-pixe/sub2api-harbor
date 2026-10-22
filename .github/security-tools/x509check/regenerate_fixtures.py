"""Offline maintainer utility: rotate synthetic fixtures using public inputs only.

Requires Python cryptography. Each run creates a fresh signing key in memory,
discards it on exit, and pins the resulting public fixtures in the Go source.
Do not run during CI: CI verifies the committed, immutable fixture bytes.
"""

import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path

import cryptography
from cryptography import x509
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import ec, ed25519
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID


DIRECTORY = Path(__file__).resolve().parent
FIXTURES = DIRECTORY / "fixtures"


def digest(data):
    return hashlib.sha256(data).hexdigest()


def public_spki(cert):
    return cert.public_key().public_bytes(
        serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo
    )


def record(cert_id, filename, cert, source):
    pem = cert.public_bytes(serialization.Encoding.PEM)
    (FIXTURES / filename).write_bytes(pem)
    return {
        "id": cert_id,
        "file": filename,
        "pem_sha256": digest(pem),
        "der_sha256": digest(cert.public_bytes(serialization.Encoding.DER)),
        "spki_sha256": digest(public_spki(cert)),
        "source": source,
    }


def main():
    manifest_path = FIXTURES / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    retained = []
    sources = {}
    for item in manifest["certificates"]:
        if item["id"].startswith(("system_", "source_")):
            pem = (FIXTURES / item["file"]).read_bytes()
            cert = x509.load_pem_x509_certificate(pem)
            assert digest(pem) == item["pem_sha256"]
            assert digest(cert.public_bytes(serialization.Encoding.DER)) == item["der_sha256"]
            assert digest(public_spki(cert)) == item["spki_sha256"]
            retained.append(item)
            sources[item["id"]] = cert
    assert digest((FIXTURES / "go-fix.json").read_bytes()) == manifest["go_fix_sha256"]
    patch = json.loads((FIXTURES / "go-fix.json").read_text(encoding="utf-8"))
    verify_patch = next(
        entry["patch"] for entry in patch["files"]
        if entry["filename"] == "src/crypto/x509/verify.go"
    )
    root_key = ed25519.Ed25519PrivateKey.generate()
    root_name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "x509check offline test root")])
    start = datetime(2020, 1, 1, tzinfo=timezone.utc)
    end = datetime(2045, 1, 1, tzinfo=timezone.utc)
    root = (
        x509.CertificateBuilder()
        .subject_name(root_name)
        .issuer_name(root_name)
        .public_key(root_key.public_key())
        .serial_number(1)
        .not_valid_before(start)
        .not_valid_after(end)
        .add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True)
        .add_extension(
            x509.KeyUsage(True, False, False, False, False, True, True, False, False),
            critical=True,
        )
        .sign(root_key, algorithm=None)
    )
    template = (
        x509.CertificateBuilder()
        .subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "x509check.invalid")]))
        .issuer_name(root_name)
        .serial_number(42)
        .not_valid_before(start)
        .not_valid_after(end)
        .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
        .add_extension(
            x509.KeyUsage(True, False, False, False, False, False, False, False, False),
            critical=True,
        )
        .add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH]), critical=False)
        .add_extension(x509.SubjectAlternativeName([x509.DNSName("x509check.invalid")]), critical=False)
    )
    generated_source = "Synthetic; generated offline by regenerate_fixtures.py; signing keys discarded"
    retained.append(record("test_root", "test-root.pem", root, generated_source))
    # All three acquired source keys are P-384; match the positive control.
    control_key = ec.generate_private_key(ec.SECP384R1())
    control = template.public_key(control_key.public_key()).sign(root_key, algorithm=None)
    retained.append(record("control", "control.pem", control, generated_source))
    probes = []
    for source_id in sorted(key for key in sources if key.startswith("source_")):
        source = sources[source_id]
        source_hash = digest(public_spki(source))
        assert source_hash in verify_patch, "source SPKI is not in the pinned official patch"
        assert isinstance(source.public_key(), ec.EllipticCurvePublicKey)
        assert source.public_key().curve.name == "secp384r1"
        cert = template.public_key(source.public_key()).sign(root_key, algorithm=None)
        assert public_spki(cert) == public_spki(source)
        cert_id = "probe_" + source_hash
        retained.append(record(cert_id, cert_id + ".pem", cert, generated_source))
        probes.append(
            {"certificate_id": cert_id, "source_id": source_id, "spki_sha256": source_hash}
        )
    assert probes, "at least one real distrusted public key is required"
    manifest["certificates"] = retained
    manifest["probes"] = probes
    manifest["generation"] = {
        "script": "regenerate_fixtures.py",
        "cryptography_version": cryptography.__version__,
        "test_root_signature": "Ed25519",
        "control_and_probe_keys": "P-384",
        "private_keys_stored": False,
        "verification_time": "2026-10-22T12:00:00Z",
    }
    encoded = (json.dumps(manifest, indent=2, ensure_ascii=True) + "\n").encode("utf-8")
    manifest_path.write_bytes(encoded)
    (DIRECTORY / "fixtures_digest.go").write_bytes(
        (
            "// Code generated by regenerate_fixtures.py; DO NOT EDIT.\n\n"
            "package main\n\n"
            'const manifestSHA256 = "' + digest(encoded) + '"\n'
        ).encode("ascii")
    )
    print(f"Pinned {len(probes)}/8 SPKI probes; manifest SHA-256 {digest(encoded)}")


if __name__ == "__main__":
    main()
