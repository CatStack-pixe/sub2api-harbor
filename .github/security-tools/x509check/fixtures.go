package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"embed"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"
)

// Public certificates only. No fixture contains a private key.
//
//go:embed fixtures/*.pem fixtures/manifest.json fixtures/go-fix.json
var fixtureFiles embed.FS

// Source: golang/go commit 7f86f957633ee31a1c17a1eb8452ca7a3d9bc141.
var distrustedSPKIs = []string{
	"ebed6475c2c2d80fcb199f3fe8dd6e6b3ce60b77eb15d4e42c3dec0c6f5b5105",
	"49dd353dc2b919a3770645117d0e60c16115e7d1bc259eefe734b00ea434fe5f",
	"e0254c7fcdc265e2f0f8a428dabb75ab4ae039b4872d62f5ee57f0877d5a8a41",
	"5c0660a2b164910d8ff589c18e14552e9e0ec237dcefe2cb736d41a4811f81a0",
	"43bffa19d6ef651451f6841e019931e96f6f2eaeff28ab4b6f1ce4576de2717b",
	"63ecefee431d49523bc1136c4e72d0206a477086caf606cd5cf025686c109cf8",
	"ec4ffa1ad0ac88081314b7aaffef6a91f97cd57291bb5ba686c5415f5eb4f6b6",
	"92c063d67e8f14db71d288e11266ffdaf69cbf115f7bab14f3bc2f68d25e0e30",
}

var verificationTime = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

type certificateRecord struct {
	ID         string `json:"id"`
	File       string `json:"file"`
	PEMSHA256  string `json:"pem_sha256"`
	DERSHA256  string `json:"der_sha256"`
	SPKISHA256 string `json:"spki_sha256"`
	Source     string `json:"source"`
}

type probeRecord struct {
	CertificateID string `json:"certificate_id"`
	SourceID      string `json:"source_id"`
	SPKISHA256    string `json:"spki_sha256"`
}

type fixtureManifest struct {
	Schema       int                 `json:"schema"`
	GoFixSHA256  string              `json:"go_fix_sha256"`
	Certificates []certificateRecord `json:"certificates"`
	Probes       []probeRecord       `json:"probes"`
}

type fixtures struct {
	certificates map[string]*x509.Certificate
	probes       []probeRecord
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func loadFixtures() (*fixtures, error) {
	return loadFixtureFiles(fixtureFiles.ReadFile)
}

func loadFixtureFiles(readFile func(string) ([]byte, error)) (*fixtures, error) {
	data, err := readFile("fixtures/manifest.json")
	if err != nil {
		return nil, err
	}
	if digest(data) != manifestSHA256 {
		return nil, fmt.Errorf("fixture manifest SHA-256 mismatch")
	}
	var manifest fixtureManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.Schema != 1 {
		return nil, fmt.Errorf("unsupported fixture schema %d", manifest.Schema)
	}
	patch, err := readFile("fixtures/go-fix.json")
	if err != nil {
		return nil, err
	}
	if digest(patch) != manifest.GoFixSHA256 {
		return nil, fmt.Errorf("Go patch snapshot SHA-256 mismatch")
	}
	f := &fixtures{certificates: make(map[string]*x509.Certificate), probes: manifest.Probes}
	for _, record := range manifest.Certificates {
		if _, exists := f.certificates[record.ID]; exists {
			return nil, fmt.Errorf("duplicate certificate ID %q", record.ID)
		}
		data, err := readFile("fixtures/" + record.File)
		if err != nil {
			return nil, err
		}
		if digest(data) != record.PEMSHA256 {
			return nil, fmt.Errorf("%s: PEM SHA-256 mismatch", record.ID)
		}
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
			return nil, fmt.Errorf("%s: expected exactly one public certificate", record.ID)
		}
		if digest(block.Bytes) != record.DERSHA256 {
			return nil, fmt.Errorf("%s: DER SHA-256 mismatch", record.ID)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", record.ID, err)
		}
		if digest(cert.RawSubjectPublicKeyInfo) != record.SPKISHA256 {
			return nil, fmt.Errorf("%s: SPKI SHA-256 mismatch", record.ID)
		}
		f.certificates[record.ID] = cert
	}
	for _, id := range []string{"test_root", "control", "system_healthy_isrg_x1", "system_comodo_aaa", "system_emsign_ecc_g3"} {
		if f.certificates[id] == nil {
			return nil, fmt.Errorf("missing required fixture %s", id)
		}
	}
	if len(f.probes) == 0 {
		return nil, fmt.Errorf("no behavioral probes")
	}
	seen := make(map[string]bool)
	for _, probe := range f.probes {
		if seen[probe.SPKISHA256] {
			return nil, fmt.Errorf("duplicate probe SPKI %s", probe.SPKISHA256)
		}
		seen[probe.SPKISHA256] = true
		known := false
		for _, hash := range distrustedSPKIs {
			known = known || hash == probe.SPKISHA256
		}
		cert, source := f.certificates[probe.CertificateID], f.certificates[probe.SourceID]
		if !known || cert == nil || source == nil {
			return nil, fmt.Errorf("invalid probe %s", probe.CertificateID)
		}
		if digest(cert.RawSubjectPublicKeyInfo) != probe.SPKISHA256 ||
			!bytes.Equal(cert.RawSubjectPublicKeyInfo, source.RawSubjectPublicKeyInfo) {
			return nil, fmt.Errorf("%s: probe/source SPKI does not match official denylist", probe.CertificateID)
		}
	}
	return f, nil
}
