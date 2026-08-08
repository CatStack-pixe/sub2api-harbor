package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type envelope struct {
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
}

func post(base, path string, value any, headers map[string]string) json.RawMessage {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	request, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		panic(err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, _ := io.ReadAll(response.Body)
	var result envelope
	if err := json.Unmarshal(responseBody, &result); err != nil || response.StatusCode >= 300 {
		panic(string(responseBody))
	}
	return result.Data
}

func main() {
	base := strings.TrimRight(os.Getenv("INGEST_URL"), "/")
	headers := map[string]string{
		"CF-Access-Client-Id": os.Getenv("CF_ACCESS_CLIENT_ID"),
		"CF-Access-Client-Secret": os.Getenv("CF_ACCESS_CLIENT_SECRET"),
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	var enrolled struct {
		ClientID string `json:"client_id"`
	}
	enrollData := post(base, "/api/v1/remote-ingest/enroll", map[string]string{
		"registration_token": os.Getenv("REGISTRATION_TOKEN"),
		"machine_name":       "remote-go-client",
		"public_key":         base64.StdEncoding.EncodeToString(publicKey),
	}, headers)
	if err := json.Unmarshal(enrollData, &enrolled); err != nil {
		panic(err)
	}
	var challenge struct {
		ID    string `json:"challenge_id"`
		Nonce string `json:"nonce"`
	}
	handshakeData := post(base, "/api/v1/remote-ingest/handshakes", map[string]string{"client_id": enrolled.ClientID}, headers)
	if err := json.Unmarshal(handshakeData, &challenge); err != nil {
		panic(err)
	}
	payload := map[string]any{
		"external_id":     fmt.Sprintf("remote-%d", time.Now().Unix()),
		"name":            "remote-openai",
		"platform":        "openai",
		"base_url":        os.Getenv("REMOTE_BASE_URL"),
		"api_key":         os.Getenv("REMOTE_API_KEY"),
		"group_name":      os.Getenv("REMOTE_GROUP_NAME"),
		"test_model":      "gpt-4.1-mini",
		"concurrency":     1,
		"priority":        0,
		"rate_multiplier": 1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	timestamp := fmt.Sprint(time.Now().Unix())
	digest := sha256.Sum256(body)
	canonical := strings.Join([]string{"sub2api-remote-ingest-v1", enrolled.ClientID, challenge.ID, challenge.Nonce, timestamp, fmt.Sprintf("%x", digest)}, "\n")
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(canonical)))
	requestHeaders := map[string]string{}
	for key, value := range headers {
		requestHeaders[key] = value
	}
	requestHeaders["X-Remote-Client-Id"] = enrolled.ClientID
	requestHeaders["X-Remote-Challenge-Id"] = challenge.ID
	requestHeaders["X-Remote-Timestamp"] = timestamp
	requestHeaders["X-Remote-Signature"] = signature
	request, err := http.NewRequest(http.MethodPost, base+"/api/v1/remote-ingest/accounts", bytes.NewReader(body))
	if err != nil {
		panic(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range requestHeaders {
		request.Header.Set(key, value)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		panic(err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusAccepted {
		panic(string(responseBody))
	}
	fmt.Println(string(responseBody))
}
