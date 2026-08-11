package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFingerprintForKey(t *testing.T) {
	key := "sk-heartbeat-test"
	fingerprint := fingerprintForKey(key)
	if !validFingerprint(fingerprint) {
		t.Fatalf("fingerprint %q should be valid", fingerprint)
	}
	if fingerprint != fingerprintForKey("  "+key+"  ") {
		t.Fatal("fingerprint should normalize surrounding whitespace")
	}
}

func TestSelectLowLatencyProxyChoosesOnlyFastTier(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"success":true,"latency_ms":` + r.URL.Path[len("/api/v1/admin/proxies/"):len(r.URL.Path)-len("/test")] + `}}`))
	}))
	defer server.Close()

	client := &sub2apiClient{baseURL: server.URL, header: "x-api-key", token: "test", http: server.Client()}
	selected, err := selectLowLatencyProxy(context.Background(), client, []proxy{{ID: 10}, {ID: 20}, {ID: 30}, {ID: 40}}, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if selected != 10 {
		t.Fatalf("selected proxy = %d, want the only fastest 10%% member", selected)
	}
}

func TestRetryDelayIsBounded(t *testing.T) {
	if got := retryDelay(1); got != 30*time.Second {
		t.Fatalf("attempt 1 delay = %s", got)
	}
	if got := retryDelay(20); got != 30*time.Minute {
		t.Fatalf("attempt 20 delay = %s", got)
	}
}

func TestProxySelectorCachesLatencyTier(t *testing.T) {
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/admin/proxies"):
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"items":[{"id":10},{"id":20},{"id":30},{"id":40}],"total":4}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/test"):
			probes.Add(1)
			id := r.URL.Path[len("/api/v1/admin/proxies/") : len(r.URL.Path)-len("/test")]
			_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"success":true,"latency_ms":` + id + `}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := &sub2apiClient{baseURL: server.URL, header: "x-api-key", token: "test", http: server.Client()}
	selector := &proxySelector{client: client, groupID: 1, workers: 2, timeout: time.Second, ttl: time.Minute}
	for range 2 {
		selected, err := selector.choose(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if selected != 10 {
			t.Fatalf("selected proxy = %d, want the only fastest 10%% member", selected)
		}
	}
	if got := probes.Load(); got != 4 {
		t.Fatalf("probe requests = %d, want one cached sweep of 4", got)
	}
}
