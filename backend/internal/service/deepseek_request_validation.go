package service

import "strings"

const deepSeekTextOnlyImageInputMessage = "DeepSeek v4 text models do not support image input"

// deepSeekTextOnlyImageRequest identifies requests that would otherwise reach
// DeepSeek's text-only v4 endpoint with an image part. Returning this decision
// before an upstream attempt prevents deterministic provider 400s from being
// retried across the account pool.
func deepSeekTextOnlyImageRequest(account *Account, model string, body []byte) bool {
	if account == nil || account.Platform != PlatformDeepseek || len(body) == 0 {
		return false
	}
	if !openAIRequestBodyMayContainImageInput(body) {
		return false
	}
	model = strings.ToLower(strings.TrimSpace(model))
	// Keep the guard conservative for future DeepSeek vision/VL models.
	return !strings.Contains(model, "-vl") &&
		!strings.Contains(model, "vision") &&
		(strings.HasPrefix(model, "deepseek-") || model == "")
}
