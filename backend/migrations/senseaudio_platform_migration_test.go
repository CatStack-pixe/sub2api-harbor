package migrations

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSenseAudioMigrationPreservesExistingPlatformConstraints(t *testing.T) {
	previous, err := FS.ReadFile("239_tierflow_platform.sql")
	require.NoError(t, err)
	current, err := FS.ReadFile("240_senseaudio_platform.sql")
	require.NoError(t, err)
	checks := regexp.MustCompile(`CHECK \((platform|target_platform) IN \(([^)]*)\)\)`)
	oldMatches := checks.FindAllStringSubmatch(string(previous), -1)
	newMatches := checks.FindAllStringSubmatch(string(current), -1)
	require.Len(t, oldMatches, 2)
	require.Len(t, newMatches, 2)
	for i, oldCheck := range oldMatches {
		require.Equal(t, oldCheck[1], newMatches[i][1])
		for _, platform := range strings.Split(oldCheck[2], ",") {
			require.Contains(t, newMatches[i][2], strings.TrimSpace(platform))
		}
		require.Contains(t, newMatches[i][2], "'senseaudio'")
	}
	require.NotContains(t, strings.ToUpper(string(current)), "DELETE FROM")
	require.NotContains(t, strings.ToUpper(string(current)), "UPDATE ")
}
