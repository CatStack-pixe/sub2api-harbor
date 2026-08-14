package service

import (
	"strings"
	"testing"
)

func TestValidateGroupGlobalPrompt(t *testing.T) {
	if err := ValidateGroupGlobalPrompt(strings.Repeat("a", MaxGroupGlobalPromptBytes)); err != nil {
		t.Fatalf("expected exact limit to be accepted: %v", err)
	}
	if err := ValidateGroupGlobalPrompt(strings.Repeat("a", MaxGroupGlobalPromptBytes+1)); err == nil {
		t.Fatal("expected prompt above byte limit to be rejected")
	}
}
