//go:build integration

package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// The new upstream constraints must accept existing fork rows immediately.
// A repair migration after a narrowing constraint cannot fix a failed upgrade.
func TestForkPlatformUpgradePreservesExistingRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "info").Run(); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("Docker is required for migration integration tests: %v", err)
		}
		t.Skip("Docker is required for migration integration tests")
	}

	image := os.Getenv("SUB2API_TEST_POSTGRES_IMAGE")
	if image == "" {
		image = "postgres:18-alpine"
	}
	container, err := postgres.Run(ctx, image,
		postgres.WithDatabase("fork_migration_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_ = container.Terminate(cleanupCtx)
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	platforms := []string{
		"anthropic", "openai", "gemini", "antigravity", "grok", "agnes",
		"deepseek", "nvidia", "tokenrhythm", "kimi", "zhipu", "chatanywhere", "glm",
		"modelscope", "dashscope", "minimax", "volcengine", "sensenova",
	}
	providers := []string{
		"openai", "anthropic", "gemini", "grok", "antigravity",
		"kimi", "zhipu", "deepseek", "sensenova",
	}
	tables := []struct {
		name   string
		column string
		values []string
	}{
		{"user_platform_quotas", "platform", platforms},
		{"composite_model_routes", "target_platform", platforms},
		{"channel_monitors", "provider", providers},
		{"channel_monitor_request_templates", "provider", providers},
	}
	for _, table := range tables {
		_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (%s TEXT NOT NULL)", table.name, table.column))
		require.NoError(t, err)
		for _, value := range table.values {
			_, err := db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES ($1)", table.name, table.column), value)
			require.NoError(t, err)
		}
	}

	// Reproduce the fork's last deployed constraints before applying the new ones.
	for _, name := range []string{
		"235_add_sensenova_platform_constraints.sql",
		"236_channel_monitor_sensenova.sql",
		"237_add_minimax_platform.sql",
		"238_opencode_go_platform.sql",
		"239_tierflow_platform.sql",
	} {
		content, err := FS.ReadFile(name)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(content))
		require.NoError(t, err, "upgrade with existing fork rows: %s", name)
	}
	for _, table := range tables {
		var count int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table.name).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, len(table.values), count, "existing rows in %s must survive", table.name)
		for _, value := range []string{"minimax", "opencode_go", "tierflow"} {
			_, err := db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES ($1)", table.name, table.column), value)
			require.NoError(t, err, "new platform %s in %s", value, table.name)
		}
		_, err = db.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (%s) VALUES ('unsupported_platform')", table.name, table.column))
		require.Error(t, err, "unknown platforms must remain rejected in %s", table.name)
	}

	// Replaying the final migration must preserve both old and new platform rows.
	content, err := FS.ReadFile("239_tierflow_platform.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(content))
	require.NoError(t, err)
}
