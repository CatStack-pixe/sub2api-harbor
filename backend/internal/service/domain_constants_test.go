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

func TestChatAnywhereDefaultModelIDs(t *testing.T) {
	want := []string{"gpt-5.5", "gpt-5.1", "gpt-4.1", "claude-sonnet-4-5", "deepseek-v3-2"}
	got := ChatAnywhereDefaultModelIDs()
	if len(got) != len(want) {
		t.Fatalf("ChatAnywhereDefaultModelIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ChatAnywhereDefaultModelIDs() = %v, want %v", got, want)
		}
	}
}

func TestTokenRhythmDefaultModelIDs(t *testing.T) {
	want := []string{"deepseek-v4-pro", "deepseek-v4-flash"}
	got := TokenRhythmDefaultModelIDs()
	if len(got) != len(want) {
		t.Fatalf("TokenRhythmDefaultModelIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TokenRhythmDefaultModelIDs() = %v, want %v", got, want)
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

func TestOfficialOpenAICompatibleDefaultModelIDs(t *testing.T) {
	tests := []struct {
		name string
		got  func() []string
		want string
	}{
		{name: "modelscope", got: ModelScopeDefaultModelIDs, want: "Qwen/Qwen3.5-397B-A17B"},
		{name: "dashscope", got: DashScopeDefaultModelIDs, want: "qwen3.5-plus"},
		{name: "minimax", got: MiniMaxDefaultModelIDs, want: "MiniMax-M3"},
		{name: "volcengine", got: VolcengineDefaultModelIDs, want: "doubao-seed-2-1-pro-260628"},
		{name: "sensenova", got: SenseNovaDefaultModelIDs, want: "SenseChat-5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.got()
			if len(got) == 0 {
				t.Fatalf("%s default model list is empty", tt.name)
			}
			found := false
			for _, modelID := range got {
				if modelID == tt.want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s default model list %v does not contain %q", tt.name, got, tt.want)
			}
		})
	}
}
