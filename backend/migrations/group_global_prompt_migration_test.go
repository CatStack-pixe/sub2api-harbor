package migrations

import (
	"strings"
	"testing"
)

func TestGroupGlobalPromptMigrationDefaultsExistingGroupsToDisabled(t *testing.T) {
	content, err := FS.ReadFile("222_group_global_prompt.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(content)
	for _, required := range []string{
		"ADD COLUMN IF NOT EXISTS global_prompt_enabled BOOLEAN NOT NULL DEFAULT FALSE",
		"ADD COLUMN IF NOT EXISTS global_prompt TEXT NOT NULL DEFAULT ''",
		"SET global_prompt_enabled = FALSE",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration missing %q", required)
		}
	}
}
