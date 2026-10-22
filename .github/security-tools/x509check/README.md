# Offline x509 security gate

Independent Go module, standard library only. The Linux executable embeds every
public certificate and performs no network requests. It checks two independent
properties:

* `crypto`: Go's actual `crypto/x509.Certificate.Verify` behavior rejects three
  of the eight public-key hashes in the pinned CVE-2026-111943 patch.
* `roots`: this fresh Linux process's `SystemCertPool` excludes Comodo AAA
  Certificate Services and emSign ECC Root CA - G3, while still trusting the
  pinned ISRG Root X1 positive control.

The program always requires patched behavior. `runtime.Version()` is reported
for diagnostics and never controls the result. Removing roots does not prove
that the Go patch is present; rebuilding Go does not remove roots from a CA
bundle.

## CI build and version regression

Run from this directory in a **Linux amd64 CI runner**, with the requested Go
toolchain already installed. No local Go installation or execution is needed.
The helper sets `GOTOOLCHAIN=local`, `GOWORK=off`, `GOPROXY=off`,
`GOSUMDB=off`, `CGO_ENABLED=0`, `GOOS=linux`, and `GOARCH=amd64`.
The `go.mod` minimum remains 1.27.2 so the old-runtime negative regression
compiles without silently selecting a patched toolchain.

On the Go 1.27.3 CI job:

```sh
python ci.py --expect patched --output /tmp/x509check-go1.27.3
```

On a separate Go 1.27.2 CI job:

```sh
python ci.py --expect vulnerable --output /tmp/x509check-go1.27.2
```

The patched job runs `go vet`, the complete Go regression suite, a static
build, ELF checks excluding `PT_INTERP` and `PT_DYNAMIC`, and the CLI's
`crypto` check. The old-runtime job requires the security regression test to
fail specifically because the probes were accepted, runs the remaining
environment/fixture tests, then requires CLI exit 1 with all three probes
accepted and both positive controls passing. A compiler failure, malformed
fixture, unexpected verifier error or rejected positive control fails the
CI helper.

The helper returns 0 when the **specified expectation** is satisfied. The
Go 1.27.2 executable itself must still return 1 for `-check crypto`; its raw
security result is never changed to success.

These commands describe the CI contract. Go builds and old/new runtime
results must be recorded by CI; they have not been executed on the development
machine.

## Run against the final container's trust store

After copying the patched static executable into the final image, run it
inside that image, as a new process, with that image's ordinary CA environment:

```sh
python -c "import subprocess; subprocess.run(['/tmp/x509check-go1.27.3', '-check', 'all', '-json'], check=True)"
```

Python is just the caller in this example. The static executable itself has no
Python, OpenSSL, shell, shared-library or network dependency and can also be
the container entrypoint for this check. The complete embedded fixture set
travels with it; fixture files are not needed alongside the executable.

| CLI option | Scope |
| --- | --- |
| `-check all` | Both checks; default |
| `-check crypto` | Explicit fixture pool only; independent of installed CAs |
| `-check roots` | Current process's Linux system pool only |
| `-json` | Versioned JSON report, individual results and numeric exit code |

| Exit | Meaning |
| --- | --- |
| 0 | Every selected check passed |
| 1 | At least one distrusted SPKI/root remains accepted or a blocked root subject remains installed |
| 2 | Invalid arguments, unsupported platform, broken fixture, missing positive control or unexpected verification error |

Errors take precedence over detected noncompliance. Reports include
`SSL_CERT_FILE`, `SSL_CERT_DIR`, platform, build Go version, fixed verification
time and actual SPKI coverage. CI must check the executable's exit status;
JSON is emitted on stdout even when the security gate fails.

`SystemCertPool` can include the Linux certificate directories as well as
`SSL_CERT_FILE`. An overridden bundle alone does not isolate those directories.
For controlled bundle tests set both variables, pointing `SSL_CERT_DIR` to an
empty existing directory, and start a fresh executable for each case.

The root check explicitly uses `ExtKeyUsageAny` and the fixed time
`2026-10-22T12:00:00Z`, inside the three pinned roots' validity intervals.
It requires the healthy root to verify successfully. Each blocked root must
produce `UnknownAuthorityError` and have no exact `RawSubject` in the Linux
pool. Subject matches are a conservative additional failure condition; subject
absence alone is insufficient. Unexpected expiry, signature, policy or other
errors produce exit 2. Native Windows/macOS pool enumeration is intentionally
reported as unsupported, rather than passing on incomplete `Subjects()`.

This checks the two pinned root identities and their ordinary same-subject
trust paths. It is not an exhaustive enumeration of every reissued certificate
using the same key under an unrelated subject. It does not inspect an already
running server's root cache, refresh that cache, or attest the Go standard
library linked into another executable. Restart rebuilt application/Caddy
processes separately after the image and CA updates.

## Behavioral fixture construction

The full official Go patch response is saved as `fixtures/go-fix.json`.
Its source is the upstream Go commit:

```text
https://github.com/golang/go/commit/7f86f957633ee31a1c17a1eb8452ca7a3d9bc141
```

`fixtures.go` records all eight upstream hashes. Three publicly available
P-384 intermediate certificates supply real blocked keys:

| Source intermediate | crt.sh certificate ID | SPKI SHA-256 |
| --- | --- | --- |
| EM DV TLS CA - G3B | 18519019583 | `ebed6475c2c2d80fcb199f3fe8dd6e6b3ce60b77eb15d4e42c3dec0c6f5b5105` |
| EM OV TLS CA - G3C | 18518875953 | `63ecefee431d49523bc1136c4e72d0206a477086caf606cd5cf025686c109cf8` |
| EM OV CS CA - G3B | 18519019578 | `ec4ffa1ad0ac88081314b7aaffef6a91f97cd57291bb5ba686c5415f5eb4f6b6` |

Each original public certificate is retained as `source_<hash>.pem`; exact
retrieval URLs are in `fixtures/manifest.json`. These public inputs are evidence
for the SPKI, not the trust anchors used by the behavioral test. The remaining
five upstream hashes are listed but not exercised. Additional downloads were
stopped after public-source timeouts/rate limits; coverage is explicitly **3/8**.

For each source public key, `regenerate_fixtures.py` issued a synthetic leaf
under a new Ed25519 test root. A normal P-384 leaf uses the same issuer, subject,
serial, signature algorithm, validity, DNS name, usages and extensions.
Only the public key and resulting signature differ. The root and normal chain
must pass actual `Verify`. Each probe's signature is independently checked,
its SPKI must match its retained public source and the upstream denylist,
and its validation constraints must match the normal leaf.

The upstream patch filters the SPKI of **every certificate in a returned
chain**, so placing an intermediate's public key in this signed leaf directly
exercises that behavior without the intermediate's private key or an external
signed leaf. This is a test of the upstream SPKI filter, not a reproduction of
the original public intermediate chain.

Only `errors.As(err, &x509.CertificateInvalidError)` with
`Reason == x509.NoValidChains` and no returned chain passes a blocked-key
probe. Acceptance means exit 1; any other verification error means exit 2.
No mock verifier, string-only version gate, unsafe certificate mutation or
test-local replacement denylist determines the expected rejection.

The synthetic signing keys existed only in Python memory during fixture
generation and were discarded. The executable performs no signing and embeds
no private keys.

## Fixed offline inputs

The Go source pins the manifest SHA-256 in `fixtures_digest.go`; the manifest
pins every PEM, certificate DER, SPKI, and the official patch snapshot.
The loader verifies them before any check. The Go tests cover corrupted
manifest, patch and PEM bytes. Runtime operation and CI require no downloads.
The local `.gitattributes` pins fixture PEM and JSON files to LF, preserving
their exact bytes even with `core.autocrlf=true`.

An independent Python audit checks the fixed hashes, original public-source
signatures, generated signatures, exact positive-control constraints and the
upstream SPKI list, without executing Go:

```sh
python verify_fixtures.py
```

This optional maintainer check requires Python `cryptography`. The Go program
and `ci.py` do not depend on that package.

Root identities were extracted from the historical Mozilla-derived certifi
2024.12.14 bundle. The historical bundle is solely a source of certificate
bytes, not the runtime trust store. Its URL and complete source bundle digest
are recorded in the manifest.

| Root | Certificate DER SHA-256 | SPKI SHA-256 |
| --- | --- | --- |
| Comodo AAA Certificate Services | `d7a7a0fb5d7e2731d771e9484ebcdef71d5f0c3e0a2948782bc83ee0ea699ef4` | `bd153ed7b0434f6886b17bce8bbe84ed340c7132d702a8f4fa318f756ecbd6f3` |
| emSign ECC Root CA - G3 | `86a1ecba089c4a8d3bbe2734c612ba341d813e043cf9e8a862cd5c57a36bbe6b` | `8d417db2dd8bf5e3084d1e3f196d583849d81bdd4c00c70b9d39369e96b8c782` |
| ISRG Root X1 positive control | `96bcec06264976f37460779acf28c5a7cfe8a3c0aae11a8ffcee05c0bddf08c6` | `0b9fa5a59eed715c26c1020c711b4f6ec42d58b0015e14337a39dad301c5afc3` |

Maintainers can intentionally rotate only the synthetic public fixtures by
running `python regenerate_fixtures.py` with Python `cryptography` installed.
This is an offline operation using the retained public inputs. It changes the
synthetic certificate bytes and updates the manifest/source pin, so its full
output must be reviewed and the two Go versions rerun in CI. Do not regenerate
fixtures as part of routine CI.

No workflow, Dockerfile, application dependency, backend file or Caddy build
is changed by this module.
