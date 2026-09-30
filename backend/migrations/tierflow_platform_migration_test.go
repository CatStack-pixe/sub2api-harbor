package migrations

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTierflowMigrationExtendsEveryExistingPlatformConstraint(t *testing.T) {
	previous, err := FS.ReadFile("238_opencode_go_platform.sql")
	require.NoError(t, err)
	current, err := FS.ReadFile("239_tierflow_platform.sql")
	require.NoError(t, err)
	checks := regexp.MustCompile(`CHECK \((platform|target_platform|provider) IN \(([^)]*)\)\)`)
	oldMatches := checks.FindAllStringSubmatch(string(previous), -1)
	newMatches := checks.FindAllStringSubmatch(string(current), -1)
	require.Len(t, oldMatches, 4)
	require.Len(t, newMatches, 4)
	for i, previous := range oldMatches {
		require.Equal(t, previous[1], newMatches[i][1])
		for _, platform := range strings.Split(previous[2], ",") {
			require.Contains(t, newMatches[i][2], strings.TrimSpace(platform), "existing platform must remain allowed")
		}
		require.Contains(t, newMatches[i][2], "'tierflow'")
	}
	require.NotContains(t, strings.ToUpper(string(current)), "DELETE FROM")
	require.NotContains(t, strings.ToUpper(string(current)), "UPDATE ")
}
