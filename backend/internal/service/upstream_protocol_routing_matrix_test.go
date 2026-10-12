//go:build unit

package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The fixture is captured through the real gateway entry points on this main
// revision, before provider profiles and resolveUpstreamProtocol were added.
// Expected results never call routing helpers from the implementation under test.
// Regenerate with python3 testdata/record_upstream_protocol_routing_main.py, which exports
// this pinned revision before compiling and recording the same matrix driver.
const routingMatrixMainRevision = "6c9fb52258492fd42ed87835ab763d3883646127"
const routingMatrixGoldenPath = "testdata/upstream_protocol_routing_main_golden.json"

type routingMatrixIngress struct {
	name          string
	path          string
	responsesBody bool
	forward       func(*OpenAIGatewayService, *gin.Context, *Account, []byte) error
}

func routingMatrixIngresses() []routingMatrixIngress {
	return []routingMatrixIngress{
		{
			name: "responses", path: "/v1/responses", responsesBody: true,
			forward: func(svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) error {
				_, err := svc.Forward(context.Background(), c, account, body)
				return err
			},
		},
		{
			name: "chat", path: "/v1/chat/completions",
			forward: func(svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) error {
				_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				return err
			},
		},
		{
			name: "chat_responses_shape", path: "/v1/chat/completions", responsesBody: true,
			forward: func(svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) error {
				_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				return err
			},
		},
		{
			name: "messages", path: "/v1/messages",
			forward: func(svc *OpenAIGatewayService, c *gin.Context, account *Account, body []byte) error {
				_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				return err
			},
		},
	}
}

type routingMatrixCase struct {
	platform    string
	apiProtocol string
	mode        string
	accountType string
	probe       string
	addresses   string
	rules       bool
	ingress     routingMatrixIngress
	model       string
}

func (tc routingMatrixCase) key() string {
	return fmt.Sprintf("%s|proto=%s|mode=%s|type=%s|probe=%s|addresses=%s|rules=%t|%s|%s",
		tc.platform, tc.apiProtocol, tc.mode, tc.accountType, tc.probe, tc.addresses, tc.rules, tc.ingress.name, tc.model)
}

func (tc routingMatrixCase) account() *Account {
	credentials := map[string]any{"api_key": "sk-test"}
	if tc.addresses == "base" || tc.addresses == "both" {
		credentials["base_url"] = "http://base.example"
	}
	if tc.addresses == "split" || tc.addresses == "both" {
		credentials["api_base_urls"] = map[string]any{
			APIProtocolChatCompletions: "http://cc.example",
			APIProtocolResponses:       "http://responses.example",
			APIProtocolAnthropic:       "http://anthropic.example",
		}
	}
	if tc.apiProtocol != "" {
		credentials["api_protocol"] = tc.apiProtocol
	}
	if tc.mode != "" {
		credentials["account_mode"] = tc.mode
	}
	if tc.rules {
		credentials["protocol_rules"] = []any{
			map[string]any{"pattern": "glm-*", "protocol": APIProtocolAnthropic},
			map[string]any{"pattern": "gpt-*", "protocol": APIProtocolResponses},
		}
	}
	if tc.platform == PlatformOpenCodeGo {
		credentials["model_mapping"] = map[string]any{
			"mapped-gpt":         "gpt-5.6-luna",
			"mapped-unsupported": "gemini-3.8-flash",
		}
	}
	extra := map[string]any{}
	switch tc.probe {
	case "yes":
		extra[openai_compat.ExtraKeyResponsesSupported] = true
	case "no":
		extra[openai_compat.ExtraKeyResponsesSupported] = false
	}
	return &Account{
		ID: 801, Name: "routing-matrix", Platform: tc.platform, Type: tc.accountType,
		Concurrency: 1, Credentials: credentials, Extra: extra,
	}
}

func (tc routingMatrixCase) body() []byte {
	switch {
	case tc.ingress.responsesBody:
		return []byte(fmt.Sprintf(`{"model":%q,"input":"hello","max_output_tokens":32,"stream":false}`, tc.model))
	case tc.ingress.name == "messages":
		return []byte(fmt.Sprintf(`{"model":%q,"max_tokens":32,"messages":[{"role":"user","content":"hello"}],"stream":false}`, tc.model))
	default:
		return []byte(fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"hello"}],"stream":false}`, tc.model))
	}
}

func routingMatrixCases() []routingMatrixCase {
	var cases []routingMatrixCase
	protocols := []string{"", APIProtocolAdaptive, APIProtocolChatCompletions, APIProtocolAnthropic, APIProtocolResponses, "invalid"}
	types := []string{AccountTypeAPIKey, AccountTypeUpstream, AccountTypeOAuth}
	probes := []string{"", "yes", "no"}
	addresses := []string{"none", "base", "split", "both"}
	add := func(platform string, modes, models []string, rulesOptions []bool, probeOptions, typeOptions []string, ingresses []routingMatrixIngress) {
		for _, proto := range protocols {
			for _, mode := range modes {
				for _, accountType := range typeOptions {
					for _, probe := range probeOptions {
						for _, address := range addresses {
							for _, rules := range rulesOptions {
								for _, ingress := range ingresses {
									for _, model := range models {
										cases = append(cases, routingMatrixCase{
											platform: platform, apiProtocol: proto, mode: mode, accountType: accountType,
											probe: probe, addresses: address, rules: rules, ingress: ingress, model: model,
										})
									}
								}
							}
						}
					}
				}
			}
		}
	}
	all := routingMatrixIngresses()
	modes := []string{"", AccountModePayG, AccountModeCoding, "invalid"}
	for _, provider := range []struct{ platform, model string }{
		{PlatformKimi, "kimi-k2"}, {PlatformZhipu, "glm-4.7"},
		{PlatformDeepseek, "deepseek-chat"}, {PlatformMiniMax, "minimax-m3"},
	} {
		add(provider.platform, modes, []string{provider.model, " " + provider.model + " "}, []bool{false}, probes, types, all)
	}
	add(PlatformOpenAI, []string{""}, []string{"gpt-5", " gpt-5 "}, []bool{false}, probes, types, all)
	add(PlatformOpenCodeGo, []string{"", AccountModeGo, AccountModeZen, "invalid"},
		[]string{"gpt-5.6-luna", "minimax-m3", "glm-5.3", "claude-sonnet-4", "qwen3.8-max", "qwen3.8-flash",
			"gemini-3.8-flash", "jev-1.13", " opencode/gpt-5.6-luna ", "mapped-gpt", "mapped-unsupported"},
		[]bool{false, true}, []string{"", "no"}, []string{AccountTypeAPIKey, AccountTypeUpstream}, all)
	// Grok has its own Responses and Chat Completions implementations; only the
	// Messages entry uses the routing logic covered by this compatibility matrix.
	add(PlatformGrok, []string{""}, []string{"grok-4", " grok-4 "}, []bool{false}, probes, types, all[3:])
	return cases
}

type routingUpstreamRequest struct {
	URL              string `json:"url"`
	Authorization    string `json:"authorization,omitempty"`
	APIKey           string `json:"api_key,omitempty"`
	AnthropicVersion string `json:"anthropic_version,omitempty"`
	BodyKind         string `json:"body_kind"`
	Model            string `json:"model"`
}

type routingObservation struct {
	Requests           []routingUpstreamRequest `json:"upstream,omitempty"`
	Error              string                   `json:"error,omitempty"`
	Panic              string                   `json:"panic,omitempty"`
	Status             int                      `json:"status"`
	ClientErrorType    string                   `json:"client_error_type,omitempty"`
	ClientErrorCode    string                   `json:"client_error_code,omitempty"`
	ClientErrorMessage string                   `json:"client_error_message,omitempty"`
}

func (o routingObservation) String() string {
	data, _ := json.Marshal(o)
	return string(data)
}

func observeRouting(tc routingMatrixCase) (obs routingObservation) {
	upstream := &httpUpstreamRecorder{err: errors.New("stop after capture")}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	body := tc.body()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, tc.ingress.path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	defer func() {
		if r := recover(); r != nil {
			obs.Panic = fmt.Sprint(r)
		}
		for i, req := range upstream.requests {
			kind := "other"
			switch {
			case gjson.GetBytes(upstream.bodies[i], "messages").Exists():
				kind = "messages"
			case gjson.GetBytes(upstream.bodies[i], "input").Exists():
				kind = "input"
			}
			obs.Requests = append(obs.Requests, routingUpstreamRequest{
				URL: req.URL.String(), Authorization: req.Header.Get("Authorization"),
				APIKey: req.Header.Get("X-Api-Key"), AnthropicVersion: req.Header.Get("Anthropic-Version"),
				BodyKind: kind, Model: gjson.GetBytes(upstream.bodies[i], "model").String(),
			})
		}
		obs.Status = c.Writer.Status()
		obs.ClientErrorType = gjson.Get(recorder.Body.String(), "error.type").String()
		obs.ClientErrorCode = gjson.Get(recorder.Body.String(), "error.code").String()
		obs.ClientErrorMessage = gjson.Get(recorder.Body.String(), "error.message").String()
	}()
	err := tc.ingress.forward(svc, c, tc.account(), body)
	if err != nil {
		obs.Error = err.Error()
	}
	return obs
}

// Cases share many outcomes. Store each distinct observation once, then one
// observation index per case in routingMatrixCases order. The key digest detects
// any change to case identity or ordering, so matrix edits require a new capture.
type routingMatrixGolden struct {
	SourceRevision string               `json:"source_revision"`
	CaseCount      int                  `json:"case_count"`
	CaseKeysSHA256 string               `json:"case_keys_sha256"`
	Observations   []routingObservation `json:"observations"`
	Results        []int                `json:"results"`
}

func routingMatrixCaseDigest(cases []routingMatrixCase) string {
	var keys strings.Builder
	for _, tc := range cases {
		keys.WriteString(tc.key())
		keys.WriteByte('\n')
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(keys.String())))
}

func captureRoutingMatrixGolden() routingMatrixGolden {
	cases := routingMatrixCases()
	golden := routingMatrixGolden{SourceRevision: routingMatrixMainRevision, CaseCount: len(cases), CaseKeysSHA256: routingMatrixCaseDigest(cases)}
	indexByObservation := make(map[string]int)
	for _, tc := range cases {
		obs := observeRouting(tc)
		key := obs.String()
		index, ok := indexByObservation[key]
		if !ok {
			index = len(golden.Observations)
			golden.Observations = append(golden.Observations, obs)
			indexByObservation[key] = index
		}
		golden.Results = append(golden.Results, index)
	}
	return golden
}

// Zhipu has no Responses endpoint. The refactor routes adaptive /responses to
// Chat Completions even for unsupported upstream/oauth account types. Both
// versions reject these accounts before transport, with these exact errors.
// Only this complete before/after observation is permitted; other no-upstream
// results and new panics still fail the golden comparison.
func matchesKnownZhipuNonAPIKeyDifference(tc routingMatrixCase, want, got routingObservation) bool {
	if tc.platform != PlatformZhipu || tc.apiProtocol != APIProtocolAdaptive || tc.ingress.name != "responses" {
		return false
	}
	var oldError string
	switch tc.accountType {
	case AccountTypeUpstream:
		oldError = "unsupported account type: upstream"
	case AccountTypeOAuth:
		oldError = "access_token not found in credentials"
	default:
		return false
	}
	return reflect.DeepEqual(want, routingObservation{Error: oldError, Status: http.StatusOK}) &&
		reflect.DeepEqual(got, routingObservation{Error: "account 801 missing api_key", Status: http.StatusOK})
}

// These expectations specify the two intentional upstream fixes from 0dccd7b6f,
// independently of production routing helpers. The captured historical fixture
// remains immutable; every other observation must still match it exactly.
func routingMatrixUpstreamFixExpectation(tc routingMatrixCase) (string, routingObservation) {
	if tc.platform != PlatformOpenCodeGo {
		return "", routingObservation{}
	}
	model := strings.TrimSpace(tc.model)
	if model == "mapped-unsupported" {
		model = "gemini-3.8-flash"
	}
	if model == "gemini-3.8-flash" || model == "jev-1.13" {
		expected := routingObservation{
			Error: "opencode unsupported model: " + model, Status: http.StatusBadRequest,
			ClientErrorType: "invalid_request_error",
			ClientErrorMessage: fmt.Sprintf("Model '%s' is not supported on OpenCode standard gateway (gemini models require Google SDK endpoint, jev models require System One endpoint)", model),
		}
		if tc.ingress.name != "messages" {
			expected.ClientErrorCode = "model_not_supported"
		}
		return "unsupported_model", expected
	}
	if model != "qwen3.8-max" || tc.mode != AccountModeZen || tc.rules ||
		(tc.apiProtocol != "" && tc.apiProtocol != APIProtocolAdaptive && tc.apiProtocol != "invalid") {
		return "", routingObservation{}
	}
	if tc.accountType != AccountTypeAPIKey {
		message := "account 801 missing api_key"
		if tc.ingress.name == "chat" || tc.ingress.name == "chat_responses_shape" {
			message = "unsupported account type: upstream"
		}
		return "qwen_chat_endpoint", routingObservation{Error: message, Status: http.StatusOK}
	}
	base := "https://opencode.ai/zen/v1"
	switch tc.addresses {
	case "base":
		base = "http://base.example/v1"
	case "split", "both":
		base = "http://cc.example/v1"
	}
	return "qwen_chat_endpoint", routingObservation{
		Requests: []routingUpstreamRequest{{
			URL: base + "/chat/completions", Authorization: "Bearer sk-test",
			BodyKind: "messages", Model: "qwen3.8-max",
		}},
		Error: "upstream error: 502 (failover)", Status: http.StatusOK,
	}
}

func TestUpstreamProtocolRoutingMatrixMatchesMainGolden(t *testing.T) {
	gin.SetMode(gin.TestMode)
	data, err := os.ReadFile(routingMatrixGoldenPath)
	require.NoError(t, err)
	var golden routingMatrixGolden
	require.NoError(t, json.Unmarshal(data, &golden))
	cases := routingMatrixCases()
	require.Equal(t, routingMatrixMainRevision, golden.SourceRevision)
	require.Equal(t, len(cases), golden.CaseCount)
	require.Equal(t, routingMatrixCaseDigest(cases), golden.CaseKeysSHA256)
	require.Len(t, golden.Results, len(cases))

	var differences []string
	var differenceRecords []struct {
		Case string             `json:"case"`
		Want routingObservation `json:"want"`
		Got  routingObservation `json:"got"`
	}
	mismatched := 0
	knownDifferences := 0
	upstreamFixCases := map[string]int{}
	for i, tc := range cases {
		index := golden.Results[i]
		require.GreaterOrEqual(t, index, 0)
		require.Less(t, index, len(golden.Observations))
		want := golden.Observations[index]
		if fix, expected := routingMatrixUpstreamFixExpectation(tc); fix != "" {
			upstreamFixCases[fix]++
			want = expected
		}
		got := observeRouting(tc)
		if reflect.DeepEqual(want, got) {
			continue
		}
		if matchesKnownZhipuNonAPIKeyDifference(tc, want, got) {
			knownDifferences++
			continue
		}
		mismatched++
		differenceRecords = append(differenceRecords, struct {
			Case string             `json:"case"`
			Want routingObservation `json:"want"`
			Got  routingObservation `json:"got"`
		}{Case: tc.key(), Want: want, Got: got})
		if len(differences) < 12 {
			differences = append(differences, fmt.Sprintf("%s\nmain: %s\ncurrent: %s", tc.key(), want, got))
		}
	}
	if path := os.Getenv("ROUTING_MATRIX_DIFFERENCES_OUTPUT"); path != "" {
		report, err := json.MarshalIndent(differenceRecords, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, report, 0o600))
	}
	require.Zero(t, mismatched, "routing differs from main %s in %d cases:\n%s", golden.SourceRevision, mismatched, strings.Join(differences, "\n"))
	// 2 account types × 4 modes × 3 probe states × 4 address configurations ×
	// 2 model spellings. A disappeared or expanded exception requires review.
	require.Equal(t, 192, knownDifferences, "unexpected change to the declared Zhipu non-API-Key difference")
	require.Equal(t, map[string]int{"unsupported_model": 9216, "qwen_chat_endpoint": 192}, upstreamFixCases,
		"intentional upstream fixes must retain their full input matrix")
}
