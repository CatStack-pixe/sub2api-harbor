package handler

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseHeartbeatCheckedAt(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      time.Time
		wantError bool
	}{
		{
			name:  "rfc3339 with fractional seconds",
			value: "2026-08-13T08:34:21.123456Z",
			want:  time.Date(2026, time.August, 13, 8, 34, 21, 123456000, time.UTC),
		},
		{
			name:  "legacy utc without offset",
			value: "2026-08-13T08:34:21.123456",
			want:  time.Date(2026, time.August, 13, 8, 34, 21, 123456000, time.UTC),
		},
		{
			name:      "invalid timestamp",
			value:     "2026-08-13 08:34:21",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHeartbeatCheckedAt(tt.value)
			if tt.wantError {
				if err == nil {
					t.Fatal("parseHeartbeatCheckedAt() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseHeartbeatCheckedAt() error = %v", err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("parseHeartbeatCheckedAt() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestHeartbeatKeyParseCheckedAtSupportsBalanceCheckedAtAlias(t *testing.T) {
	legacy := heartbeatKey{BalanceCheckedAt: "2026-08-13T08:34:21.123456"}
	got, err := legacy.parseCheckedAt()
	if err != nil {
		t.Fatalf("parseCheckedAt() error = %v", err)
	}
	want := time.Date(2026, time.August, 13, 8, 34, 21, 123456000, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseCheckedAt() = %s, want %s", got, want)
	}

	conflicting := heartbeatKey{
		CheckedAt:        "2026-08-13T08:34:21Z",
		BalanceCheckedAt: "2026-08-13T08:35:21Z",
	}
	if _, err := conflicting.parseCheckedAt(); err == nil {
		t.Fatal("parseCheckedAt() error = nil for conflicting timestamps")
	}
}

func TestHeartbeatRequestSupportsPerKeyGroupID(t *testing.T) {
	var request heartbeatRequest
	if err := json.Unmarshal([]byte(`{"session_key":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","ts":1,"keys":[{"fp":"aaaaaaaaaaaaaaaaaaaaaaaa","provider":"ds","balance":1,"checked_at":"2026-08-13T08:34:21Z","group_id":13}]}`), &request); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if request.Keys[0].GroupID == nil || *request.Keys[0].GroupID != 13 {
		t.Fatalf("group_id = %#v, want 13", request.Keys[0].GroupID)
	}
}
