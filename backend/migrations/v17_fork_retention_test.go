package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// Every migration shipped by Harbor v0.2.16 remains byte-identical so an
// existing production database can upgrade without checksum repair.
func TestV17RetainsAllDeployedHarborMigrations(t *testing.T) {
	data, err := os.ReadFile("testdata/v17_harbor_migration_checksums.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Baseline string `json:"baseline"`
		Migrations []struct {
			Filename string `json:"filename"`
			SHA256 string `json:"sha256"`
		} `json:"migrations"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Baseline != "8b294102d964ca0f30f25a3320f94694e8854450" || len(fixture.Migrations) < 316 {
		t.Fatalf("unexpected deployed migration baseline: %s (%d files)", fixture.Baseline, len(fixture.Migrations))
	}
	for _, migration := range fixture.Migrations {
		content, err := os.ReadFile(migration.Filename)
		if err != nil {
			t.Fatal(err)
		}
		actual := sha256.Sum256(content)
		if hex.EncodeToString(actual[:]) != migration.SHA256 {
			t.Errorf("deployed migration changed: %s", migration.Filename)
		}
	}
}
