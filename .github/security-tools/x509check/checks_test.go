package main

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"strings"
	"testing"
)

func testFixtures(t *testing.T) *fixtures {
	t.Helper()
	f, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPatchedCryptoX509RejectsDistrustedSPKIs(t *testing.T) {
	f := testFixtures(t)
	results := checkCrypto(f)
	if len(results) != 2+len(f.probes) {
		t.Fatalf("missing controls or probes: %+v", results)
	}
	for _, r := range results {
		t.Run(r.Name, func(t *testing.T) {
			if r.Status != "pass" {
				t.Fatalf("%s: %s", r.Status, r.Detail)
			}
		})
	}
}

func TestSystemRootExclusionWithControlledPools(t *testing.T) {
	f := testFixtures(t)
	for _, id := range []string{"clean", "system_comodo_aaa", "system_emsign_ecc_g3"} {
		t.Run(id, func(t *testing.T) {
			pool := x509.NewCertPool()
			pool.AddCert(f.certificates["system_healthy_isrg_x1"])
			want := 0
			if id != "clean" {
				pool.AddCert(f.certificates[id])
				want = 1
			}
			results := checkSystemRoots(f, pool, "linux")
			if got := exitCode(results); got != want {
				t.Fatalf("exit %d, want %d: %+v", got, want, results)
			}
			if id != "clean" {
				for _, r := range results {
					if r.Name == id && (r.Status != "fail" || !strings.Contains(r.Detail, "is trusted")) {
						t.Fatalf("installed root was not identified by actual Verify: %+v", r)
					}
				}
			}
		})
	}
}

func TestSystemRootEnvironmentMustBeUsable(t *testing.T) {
	f := testFixtures(t)
	if got := exitCode(checkSystemRoots(f, x509.NewCertPool(), "linux")); got != 2 {
		t.Fatalf("empty system pool exit=%d, want 2", got)
	}
	onlyBad := x509.NewCertPool()
	onlyBad.AddCert(f.certificates["system_comodo_aaa"])
	if got := exitCode(checkSystemRoots(f, onlyBad, "linux")); got != 2 {
		t.Fatalf("missing positive control exit=%d, want 2", got)
	}
	if got := exitCode(checkSystemRoots(f, onlyBad, "darwin")); got != 2 {
		t.Fatalf("incomplete native Subjects exit=%d, want 2", got)
	}
}

func TestFixtureTamperingFailsClosed(t *testing.T) {
	for _, name := range []string{"fixtures/manifest.json", "fixtures/go-fix.json", "fixtures/control.pem"} {
		t.Run(name, func(t *testing.T) {
			readFile := func(path string) ([]byte, error) {
				data, err := fixtureFiles.ReadFile(path)
				if err == nil && path == name {
					data = append(append([]byte{}, data...), '\n')
				}
				return data, err
			}
			if _, err := loadFixtureFiles(readFile); err == nil {
				t.Fatal("tampered fixture was accepted")
			}
		})
	}
}

func TestCLIJSONMatchesExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-check", "crypto", "-json"}, &stdout, &stderr)
	var r report
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("invalid JSON: %v; stderr=%s", err, stderr.String())
	}
	if code != r.ExitCode || code != 0 || r.CoveredSPKIs == 0 || r.OfficialSPKIs != 8 {
		t.Fatalf("unexpected report: exit=%d %+v", code, r)
	}
}

func TestCLIRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"-check", "missing"}, {"unexpected"}, {"-unknown"}} {
		var stdout, stderr bytes.Buffer
		if got := run(args, &stdout, &stderr); got != 2 {
			t.Fatalf("args %v: exit %d, want 2", args, got)
		}
	}
}
