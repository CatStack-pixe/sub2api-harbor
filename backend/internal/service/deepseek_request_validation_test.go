//go:build unit

package service

import "testing"

func TestDeepSeekTextOnlyImageRequest(t *testing.T) {
	t.Parallel()

	imageBody := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}]}]}`)
	account := &Account{Platform: PlatformDeepseek}

	if !deepSeekTextOnlyImageRequest(account, "deepseek-v4-flash-0731", imageBody) {
		t.Fatal("expected text-only DeepSeek model with image input to be rejected")
	}
	if deepSeekTextOnlyImageRequest(account, "deepseek-v4-vl", imageBody) {
		t.Fatal("vision-capable DeepSeek model must remain eligible")
	}
	if deepSeekTextOnlyImageRequest(&Account{Platform: PlatformZhipu}, "deepseek-v4-flash-0731", imageBody) {
		t.Fatal("non-DeepSeek account must not be rejected by DeepSeek guard")
	}
}

func TestDeepSeekTextOnlyImageRequestDetectsEarlierMessage(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/a.png"}}]},{"role":"assistant","content":"I can help"},{"role":"user","content":"continue"}]}`)
	if !deepSeekTextOnlyImageRequest(&Account{Platform: PlatformDeepseek}, "deepseek-v4-flash", body) {
		t.Fatal("expected an image in message history to be rejected for text-only DeepSeek models")
	}
}

func TestDeepSeekTextOnlyImageRequestResponsesInput(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"deepseek-v4-pro-0813","input":[{"role":"user","content":[{"type":"input_text","text":"describe"},{"type":"input_image","image_url":"https://example.test/a.png"}]}]}`)
	if !deepSeekTextOnlyImageRequest(&Account{Platform: PlatformDeepseek}, "deepseek-v4-pro-0813", body) {
		t.Fatal("expected Responses input_image to trigger the DeepSeek text-only guard")
	}
}
