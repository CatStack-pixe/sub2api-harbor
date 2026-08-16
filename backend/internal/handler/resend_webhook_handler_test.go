package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVerifyResendWebhook(t *testing.T) {
	key := []byte("test webhook signing key")
	secret := "whsec_" + base64.StdEncoding.EncodeToString(key)
	id := "msg_test_event"
	now := time.Unix(1_700_000_000, 0)
	timestamp := "1700000000"
	body := []byte(`{"type":"email.delivered"}`)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(id + "." + timestamp + "."))
	_, _ = mac.Write(body)
	signature := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))

	require.NoError(t, verifyResendWebhook(secret, id, timestamp, signature, body, now))
	require.Error(t, verifyResendWebhook(secret, id, timestamp, signature, []byte(`{}`), now))
	require.Error(t, verifyResendWebhook(secret, id, timestamp, signature, body, now.Add(6*time.Minute)))
}
