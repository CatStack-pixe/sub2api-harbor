"""Audit the fixed public fixtures offline with Python cryptography; does not run Go."""

import hashlib
import json
from pathlib import Path
import re
from datetime import datetime, timezone

from cryptography import x509
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import ec, ed25519, rsa


DIRECTORY = Path(__file__).resolve().parent


def digest(data):
    return hashlib.sha256(data).hexdigest()


def spki(cert):
    return cert.public_key().public_bytes(
        serialization.Encoding.DER, serialization.PublicFormat.SubjectPublicKeyInfo
    )


def check_signature(cert, issuer):
    assert cert.issuer == issuer.subject
    key = issuer.public_key()
    if isinstance(key, ed25519.Ed25519PublicKey):
        key.verify(cert.signature, cert.tbs_certificate_bytes)
    elif isinstance(key, rsa.RSAPublicKey):
        # AAA's historical root self-signature uses SHA-1. Verify it directly;
        # this is an identity/signature audit, not the Go chain acceptance test.
        key.verify(
            cert.signature, cert.tbs_certificate_bytes,
            cert.signature_algorithm_parameters, cert.signature_hash_algorithm,
        )
    elif isinstance(key, ec.EllipticCurvePublicKey):
        key.verify(
            cert.signature, cert.tbs_certificate_bytes,
            ec.ECDSA(cert.signature_hash_algorithm),
        )
    else:
        raise AssertionError("unexpected fixture public-key algorithm")


def main():
    raw = (DIRECTORY / "fixtures/manifest.json").read_bytes()
    pin = re.search(
        r'"([0-9a-f]{64})"', (DIRECTORY / "fixtures_digest.go").read_text(encoding="utf-8")
    ).group(1)
    assert digest(raw) == pin
    manifest = json.loads(raw)
    patch = (DIRECTORY / "fixtures/go-fix.json").read_bytes()
    assert digest(patch) == manifest["go_fix_sha256"]
    verify_patch = next(
        item["patch"] for item in json.loads(patch)["files"]
        if item["filename"] == "src/crypto/x509/verify.go"
    )
    official = set(re.findall(r"[0-9a-f]{64}", verify_patch))
    implementation = set(re.findall(
        r"[0-9a-f]{64}", (DIRECTORY / "fixtures.go").read_text(encoding="utf-8")
    ))
    assert official == implementation and len(official) == 8
    certs = {}
    for record in manifest["certificates"]:
        pem = (DIRECTORY / "fixtures" / record["file"]).read_bytes()
        assert b"\r" not in pem and pem.count(b"BEGIN CERTIFICATE") == 1
        assert b"PRIVATE KEY" not in pem and digest(pem) == record["pem_sha256"]
        cert = x509.load_pem_x509_certificate(pem)
        assert digest(cert.public_bytes(serialization.Encoding.DER)) == record["der_sha256"]
        assert digest(spki(cert)) == record["spki_sha256"]
        assert record["id"] not in certs
        certs[record["id"]] = cert
    at = datetime(2026, 10, 22, 12, tzinfo=timezone.utc)
    for name in ("test_root", "system_comodo_aaa", "system_emsign_ecc_g3", "system_healthy_isrg_x1"):
        cert = certs[name]
        check_signature(cert, cert)
        assert cert.not_valid_before_utc < at < cert.not_valid_after_utc
    root, control = certs["test_root"], certs["control"]
    check_signature(control, root)
    seen = set()
    for probe in manifest["probes"]:
        cert, source = certs[probe["certificate_id"]], certs[probe["source_id"]]
        assert probe["spki_sha256"] in official and probe["spki_sha256"] not in seen
        seen.add(probe["spki_sha256"])
        assert digest(spki(cert)) == probe["spki_sha256"] and spki(cert) == spki(source)
        assert cert.subject == control.subject and cert.issuer == control.issuer
        assert cert.serial_number == control.serial_number
        assert cert.signature_algorithm_oid == control.signature_algorithm_oid
        assert list(cert.extensions) == list(control.extensions)
        assert cert.not_valid_before_utc == control.not_valid_before_utc
        assert cert.not_valid_after_utc == control.not_valid_after_utc
        assert cert.not_valid_before_utc < at < cert.not_valid_after_utc
        check_signature(cert, root)
        check_signature(source, certs["system_comodo_aaa"])
        print("Verified public source, signature and exact probe/control constraints:", probe["spki_sha256"])
    assert seen
    print(f"PASS: {len(certs)} public certificates; {len(seen)}/8 real SPKI probes; manifest SHA-256 {pin}")
    print("Go compilation, crypto/x509 behavior and Linux system roots remain CI checks.")


if __name__ == "__main__":
    main()
