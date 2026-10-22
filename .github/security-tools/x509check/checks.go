package main

import (
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"
	"reflect"
	"runtime"
)

type checkResult struct {
	Check      string `json:"check"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	SPKISHA256 string `json:"spki_sha256,omitempty"`
}

func result(check, name, status, detail string) checkResult {
	return checkResult{Check: check, Name: name, Status: status, Detail: detail}
}

func checkCrypto(f *fixtures) []checkResult {
	const group = "crypto"
	root, control := f.certificates["test_root"], f.certificates["control"]
	pool := x509.NewCertPool()
	pool.AddCert(root)
	opts := x509.VerifyOptions{
		Roots: pool, CurrentTime: verificationTime,
		DNSName: "x509check.invalid", KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if err := root.CheckSignatureFrom(root); err != nil {
		return []checkResult{result(group, "fixture-root", "error", err.Error())}
	}
	chains, err := root.Verify(x509.VerifyOptions{
		Roots: pool, CurrentTime: verificationTime, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil || len(chains) != 1 || len(chains[0]) != 1 {
		return []checkResult{result(group, "fixture-root", "error", fmt.Sprintf("normal trusted root rejected: %v", err))}
	}
	results := []checkResult{result(group, "fixture-root", "pass", "normal explicitly trusted root accepted")}
	chains, err = control.Verify(opts)
	if err != nil || len(chains) != 1 || len(chains[0]) != 2 {
		return append(results, result(group, "control-chain", "error", fmt.Sprintf("normal two-certificate chain rejected: %v", err)))
	}
	results = append(results, result(group, "control-chain", "pass", "normal leaf -> test root accepted at fixed time and DNS name"))
	for _, probe := range f.probes {
		cert := f.certificates[probe.CertificateID]
		// The probes share the control's issuer, name, validity and extensions.
		// Only their public key (not the issuer/signing key) is distrusted.
		if err := cert.CheckSignatureFrom(root); err != nil {
			results = append(results, result(group, probe.CertificateID, "error", "invalid fixture signature: "+err.Error()))
			continue
		}
		if !bytes.Equal(cert.RawIssuer, control.RawIssuer) ||
			!bytes.Equal(cert.RawSubject, control.RawSubject) ||
			!cert.NotBefore.Equal(control.NotBefore) || !cert.NotAfter.Equal(control.NotAfter) ||
			cert.SignatureAlgorithm != control.SignatureAlgorithm ||
			!reflect.DeepEqual(cert.Extensions, control.Extensions) {
			results = append(results, result(group, probe.CertificateID, "error", "probe differs from positive control in validation constraints"))
			continue
		}
		chains, err := cert.Verify(opts)
		r := result(group, probe.CertificateID, "error", "unexpected Verify outcome")
		r.SPKISHA256 = probe.SPKISHA256
		var invalid x509.CertificateInvalidError
		switch {
		case err == nil:
			r.Status = "fail"
			r.Detail = fmt.Sprintf("distrusted SPKI accepted (%d chain(s)); crypto/x509 patch missing", len(chains))
		case errors.As(err, &invalid) && invalid.Reason == x509.NoValidChains && len(chains) == 0:
			r.Status = "pass"
			r.Detail = "crypto/x509 rejected verified fixture with CertificateInvalidError/NoValidChains"
		default:
			r.Detail = fmt.Sprintf("unexpected verification error (not proof of patch): %T: %v", err, err)
		}
		results = append(results, r)
	}
	return results
}

func checkSystemRoots(f *fixtures, pool *x509.CertPool, platform string) []checkResult {
	const group = "roots"
	if platform != "linux" {
		return []checkResult{result(group, "system-pool", "error", "root exclusion requires Linux: native Windows/macOS pool Subjects may be incomplete")}
	}
	if pool == nil || len(pool.Subjects()) == 0 {
		return []checkResult{result(group, "system-pool", "error", "system pool is missing or empty")}
	}
	opts := x509.VerifyOptions{
		Roots: pool, CurrentTime: verificationTime, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
	healthy := f.certificates["system_healthy_isrg_x1"]
	chains, err := healthy.Verify(opts)
	results := []checkResult{}
	if err != nil || len(chains) == 0 {
		results = append(results, result(group, "system-positive-control", "error", fmt.Sprintf("ISRG Root X1 is not trusted by this pool: %v", err)))
	} else {
		results = append(results, result(group, "system-positive-control", "pass", "SystemCertPool accepts pinned ISRG Root X1 at fixed time"))
	}
	for _, id := range []string{"system_comodo_aaa", "system_emsign_ecc_g3"} {
		cert := f.certificates[id]
		subjectPresent := false
		for _, subject := range pool.Subjects() {
			subjectPresent = subjectPresent || bytes.Equal(subject, cert.RawSubject)
		}
		chains, err := cert.Verify(opts)
		r := result(group, id, "error", "unexpected root verification outcome")
		r.SPKISHA256 = digest(cert.RawSubjectPublicKeyInfo)
		var unknown x509.UnknownAuthorityError
		switch {
		case err == nil:
			r.Status = "fail"
			r.Detail = fmt.Sprintf("distrusted root is trusted by SystemCertPool (%d chain(s))", len(chains))
		case subjectPresent:
			r.Status = "fail"
			r.Detail = fmt.Sprintf("distrusted root RawSubject remains in SystemCertPool; Verify: %v", err)
		case errors.As(err, &unknown):
			r.Status = "pass"
			r.Detail = "pinned root rejected with UnknownAuthorityError and RawSubject absent from Linux pool"
		default:
			r.Detail = fmt.Sprintf("unexpected root error (not proof of exclusion): %T: %v", err, err)
		}
		results = append(results, r)
	}
	return results
}

func systemRootChecks(f *fixtures) []checkResult {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return []checkResult{result("roots", "system-pool", "error", err.Error())}
	}
	return checkSystemRoots(f, pool, runtime.GOOS)
}

func exitCode(results []checkResult) int {
	code := 0
	for _, r := range results {
		switch r.Status {
		case "error":
			return 2
		case "fail":
			code = 1
		}
	}
	return code
}
