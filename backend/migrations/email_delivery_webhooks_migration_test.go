package migrations

import (
	"strings"
	"testing"
)

func TestEmailDeliveryWebhookMigrationIsEmbedded(t *testing.T) {
	content, err := FS.ReadFile("224_email_delivery_webhooks.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for _, expected := range []string{"email_deliveries", "email_delivery_events", "UNIQUE (provider, event_id)"} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("migration missing %q", expected)
		}
	}
}
