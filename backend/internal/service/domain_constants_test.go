//go:build unit

package service

import "testing"

func TestDeepSeekDefaultModelIDs(t *testing.T) {
	got := DeepSeekDefaultModelIDs()
	want := []string{"deepseek-v4-pro", "deepseek-v4-flash"}

	if len(got) != len(want) {
		t.Fatalf("DeepSeekDefaultModelIDs() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("DeepSeekDefaultModelIDs() = %v, want %v", got, want)
		}
	}
}

func TestNvidiaDefaultModelIDs(t *testing.T) {
	got := NvidiaDefaultModelIDs()
	want := []string{
		"nvidia/llama-3.1-nemotron-nano-8b-v1",
		"meta/llama-3.1-8b-instruct",
		"meta/llama-3.1-70b-instruct",
		"meta/llama-3.3-70b-instruct",
	}

	if len(got) != len(want) {
		t.Fatalf("NvidiaDefaultModelIDs() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("NvidiaDefaultModelIDs() = %v, want %v", got, want)
		}
	}
}

// TestSettingKeyDefaultPlatformQuotas 验证新的系统层 JSON key 常量值正确。
func TestSettingKeyDefaultPlatformQuotas(t *testing.T) {
	if SettingKeyDefaultPlatformQuotas != "default_platform_quotas" {
		t.Errorf("SettingKeyDefaultPlatformQuotas = %q, want %q",
			SettingKeyDefaultPlatformQuotas, "default_platform_quotas")
	}
}

// TestSettingKeyAuthSourcePlatformQuotas 验证新的 auth-source JSON key 函数返回值正确。
func TestSettingKeyAuthSourcePlatformQuotas(t *testing.T) {
	if got := SettingKeyAuthSourcePlatformQuotas("email"); got != "auth_source_default_email_platform_quotas" {
		t.Fatalf("got %q, want %q", got, "auth_source_default_email_platform_quotas")
	}
	if got := SettingKeyAuthSourcePlatformQuotas("dingtalk"); got != "auth_source_default_dingtalk_platform_quotas" {
		t.Fatalf("got %q, want %q", got, "auth_source_default_dingtalk_platform_quotas")
	}
}
