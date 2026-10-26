package migrations

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Recorded from the deployed v0.2.15-harbor.2 Git tree, before the v16 import.
// Never regenerate this fixture from the implementation being tested.
//
//go:embed testdata/v16_harbor_migration_checksums.json
var v16HarborMigrationChecksums []byte

func TestV16UpgradePreservesHistoricalMigrationChecksums(t *testing.T) {
	var baseline struct {
		SourceRevision string `json:"source_revision"`
		Migrations     map[string]struct {
			FileSHA256      string `json:"file_sha256"`
			MigrationSHA256 string `json:"migration_sha256"`
		} `json:"migrations"`
	}
	require.NoError(t, json.Unmarshal(v16HarborMigrationChecksums, &baseline))
	require.Equal(t, "c032824fe1a3322ac4b661f73f25e0eaf21a9e84", baseline.SourceRevision)
	require.NotEmpty(t, baseline.Migrations)

	for name, expected := range baseline.Migrations {
		t.Run(name, func(t *testing.T) {
			content, err := FS.ReadFile(name)
			require.NoError(t, err, "a deployed migration must remain present")
			raw := sha256.Sum256(content)
			require.Equal(t, expected.FileSHA256, hex.EncodeToString(raw[:]), "deployed SQL bytes must remain unchanged")
			trimmed := sha256.Sum256([]byte(strings.TrimSpace(string(content))))
			require.Equal(t, expected.MigrationSHA256, hex.EncodeToString(trimmed[:]), "schema_migrations checksum must remain unchanged")
		})
	}

	names, err := fs.Glob(FS, "*.sql")
	require.NoError(t, err)
	var historical []string
	for _, name := range names {
		if name <= "242_drop_platform_check_constraints.sql" {
			historical = append(historical, name)
			require.Contains(t, baseline.Migrations, name, "new migrations must be appended after the deployed baseline")
		}
	}
	require.Len(t, historical, len(baseline.Migrations))
}

func TestV16UpgradeOrdersBoth242MigrationsByFullFilename(t *testing.T) {
	names, err := fs.Glob(FS, "*.sql")
	require.NoError(t, err)
	sort.Strings(names)
	var tail []string
	for _, name := range names {
		if name >= "242_drop_platform_check_constraints.sql" && name <= "245_gemini_cached_contents_mutation_lease.sql" {
			tail = append(tail, name)
		}
	}
	require.Equal(t, []string{
		"242_drop_platform_check_constraints.sql",
		"242_group_i18n.sql",
		"243_gemini_cached_contents.sql",
		"244_gemini_cached_contents_channel_usage.sql",
		"245_gemini_cached_contents_mutation_lease.sql",
	}, tail, "242 is not a unique version: the runner records and orders complete filenames")
}
