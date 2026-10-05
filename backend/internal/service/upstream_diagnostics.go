package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// UpstreamTraceRequest is intentionally single-shot and non-streaming.
// Credentials are held only for the duration of the request.
type UpstreamTraceRequest struct {
	UpstreamBaseURL   string `json:"upstream_base_url"`
	APIKey            string `json:"api_key"`
	LoginEmail        string `json:"login_email,omitempty"`
	LoginPassword     string `json:"login_password,omitempty"`
	LoginTOTP         string `json:"login_totp,omitempty"`
	ManagementToken   string `json:"management_token,omitempty"`
	UpstreamKeyID     *int64 `json:"upstream_key_id,omitempty"`
	UpstreamGroupID   *int64 `json:"upstream_group_id,omitempty"`
	RequestModel      string `json:"request_model"`
	Protocol          string `json:"protocol,omitempty"`
	Prompt            string `json:"prompt,omitempty"`
	ExpectedGroupName string `json:"expected_group_name,omitempty"`
}

type UpstreamTraceEvent struct {
	At      time.Time      `json:"at"`
	Phase   string         `json:"phase"`
	Status  int            `json:"status,omitempty"`
	Success bool           `json:"success"`
	Message string         `json:"message,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

type UpstreamTraceResult struct {
	TraceID       string               `json:"trace_id"`
	AccountID     int64                `json:"account_id"`
	LocalAccount  map[string]any       `json:"local_account,omitempty"`
	StartedAt     time.Time            `json:"started_at"`
	FinishedAt    time.Time            `json:"finished_at"`
	Events        []UpstreamTraceEvent `json:"events"`
	Models        []string             `json:"models,omitempty"`
	RequestModel  string               `json:"request_model"`
	Protocol      string               `json:"protocol"`
	RequestPath   string               `json:"request_path,omitempty"`
	ResponseModel string               `json:"response_model,omitempty"`
	Usage         map[string]any       `json:"usage,omitempty"`
	Billing       map[string]any       `json:"billing,omitempty"`
	Verdict       map[string]any       `json:"verdict"`
	ErrorClass    string               `json:"error_class,omitempty"`
}

type UpstreamAuthorizationProbeRequest struct {
	UpstreamBaseURL string `json:"upstream_base_url"`
	ManagementToken string `json:"management_token"`
	ProbeGroupID    int64  `json:"probe_group_id"`
}

type UpstreamAuthorizationProbeResult struct {
	TraceID           string `json:"trace_id"`
	CreatedKeyID      int64  `json:"created_key_id,omitempty"`
	RequestedGroupID  int64  `json:"requested_group_id"`
	CreatedGroupID    int64  `json:"created_group_id,omitempty"`
	ReadbackGroupID   int64  `json:"readback_group_id,omitempty"`
	CreateStatus      int    `json:"create_status,omitempty"`
	ReadbackStatus    int    `json:"readback_status,omitempty"`
	CleanupStatus     int    `json:"cleanup_status,omitempty"`
	CleanupOK         bool   `json:"cleanup_ok"`
	GroupIDApplied    bool   `json:"group_id_applied"`
	AuthorizationTest string `json:"authorization_test"`
	Error             string `json:"error,omitempty"`
	CleanupWarning    string `json:"cleanup_warning,omitempty"`
}

const upstreamTraceMaxResponseBytes = 1 << 20

// RunUpstreamTrace retrieves the upstream catalog and sends exactly one
// non-streaming request. It never changes an upstream key, group, quota,
// blacklist, or production routing state.
func (s *AccountTestService) RunUpstreamTrace(ctx context.Context, accountID int64, req UpstreamTraceRequest) (*UpstreamTraceResult, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, errors.New("诊断服务未初始化：上游 HTTP 客户端不可用")
	}
	base, err := s.validateTraceBaseURL(req.UpstreamBaseURL)
	if err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(req.APIKey)
	model := strings.TrimSpace(req.RequestModel)
	if model == "" {
		return nil, errors.New("请填写请求模型")
	}
	if len(apiKey) > 8192 || len(strings.TrimSpace(req.LoginEmail)) > 320 ||
		len(req.LoginPassword) > 8192 || len(req.LoginTOTP) > 32 ||
		len(strings.TrimSpace(req.ManagementToken)) > 8192 {
		return nil, errors.New("登录凭据或 API Key 超过长度限制")
	}
	if len(model) > 256 || len(req.Prompt) > 2000 {
		return nil, errors.New("请求模型或提示词超过长度限制")
	}
	protocol := normalizeTraceProtocol(req.Protocol)
	result := &UpstreamTraceResult{
		TraceID:      uuid.NewString(),
		AccountID:    accountID,
		StartedAt:    time.Now().UTC(),
		RequestModel: model,
		Protocol:     protocol,
		Verdict: map[string]any{
			"request_model_sent_once": true,
			"side_effects":            "none",
		},
	}
	if s.accountRepo != nil {
		localAccount, accountErr := s.accountRepo.GetByID(ctx, accountID)
		if accountErr != nil {
			return nil, fmt.Errorf("load local account: %w", accountErr)
		}
		if localAccount != nil {
			result.LocalAccount = map[string]any{
				"name": localAccount.Name, "platform": localAccount.Platform, "type": localAccount.Type,
			}
		}
	}
	defer func() {
		result.FinishedAt = time.Now().UTC()
	}()
	addEvent := func(phase string, status int, success bool, message string, details map[string]any) {
		result.Events = append(result.Events, UpstreamTraceEvent{
			At: time.Now().UTC(), Phase: phase, Status: status,
			Success: success, Message: message, Details: details,
		})
	}

	if apiKey == "" && strings.TrimSpace(req.LoginEmail) != "" && req.LoginPassword != "" &&
		req.UpstreamGroupID != nil && *req.UpstreamGroupID > 0 {
		selectedKey, selectedID, loginEvent, selectErr := s.selectTraceKeyByLogin(
			ctx, base, req.LoginEmail, req.LoginPassword, req.LoginTOTP, *req.UpstreamGroupID,
		)
		if loginEvent != nil {
			result.Events = append(result.Events, *loginEvent)
		}
		if selectErr != nil {
			result.ErrorClass = "authentication"
			result.Verdict["key_selection"] = "failed"
			return result, nil
		}
		apiKey = selectedKey
		if selectedID > 0 {
			req.UpstreamKeyID = &selectedID
		}
		result.Verdict["key_selection"] = "passed"
	} else if apiKey == "" && strings.TrimSpace(req.ManagementToken) != "" &&
		req.UpstreamGroupID != nil && *req.UpstreamGroupID > 0 {
		selectedKey, selectedID, keyEvent, selectErr := s.selectTraceKey(
			ctx, base, req.ManagementToken, *req.UpstreamGroupID,
		)
		if keyEvent != nil {
			result.Events = append(result.Events, *keyEvent)
		}
		if selectErr != nil {
			result.ErrorClass = "authentication"
			result.Verdict["key_selection"] = "failed"
			return result, nil
		}
		apiKey = selectedKey
		if selectedID > 0 {
			req.UpstreamKeyID = &selectedID
		}
		result.Verdict["key_selection"] = "passed"
	}
	if apiKey == "" {
		return nil, errors.New("请填写 API Key，或填写上游邮箱、密码和分组 ID")
	}

	modelsResponse, modelsValue, err := s.traceRequest(ctx, http.MethodGet, traceDataURL(base, "/v1/models"), apiKey, nil)
	if err != nil {
		result.ErrorClass = classifyTraceError(modelsResponse.StatusCode, err.Error())
		addEvent("model_discovery", modelsResponse.StatusCode, false, err.Error(), traceResponseDetails(modelsResponse, modelsValue, apiKey))
		result.Verdict["model_discovery"] = "failed"
		return result, nil
	}
	result.Models = extractTraceModels(modelsValue)
	result.Verdict["model_discovery"] = "passed"
	result.Verdict["model_list_contains_request_model"] = traceContainsModel(result.Models, model)
	addEvent("model_discovery", modelsResponse.StatusCode, true, "model catalog retrieved", map[string]any{
		"models":                    result.Models,
		"contains_request_model":    result.Verdict["model_list_contains_request_model"],
		"request_model":             model,
		"request_body_was_not_sent": true,
	})

	var keyBefore map[string]any
	var usageBefore []map[string]any
	if strings.TrimSpace(req.ManagementToken) != "" && req.UpstreamKeyID != nil && *req.UpstreamKeyID > 0 {
		keyBefore, _ = s.traceReadManagementKey(ctx, base, req.ManagementToken, *req.UpstreamKeyID, apiKey)
		usageBefore, _ = s.traceReadManagementUsage(ctx, base, req.ManagementToken, *req.UpstreamKeyID, apiKey)
		result.Billing = map[string]any{
			"key_before":         summarizeTraceKey(keyBefore),
			"usage_before_count": len(usageBefore),
		}
		addEvent("billing_before", http.StatusOK, keyBefore != nil, "read-only key and usage baseline", map[string]any{
			"key":             summarizeTraceKey(keyBefore),
			"usage_row_count": len(usageBefore),
		})
	}

	body, path := traceRequestBody(protocol, model, req.Prompt)
	result.RequestPath = path
	response, responseValue, err := s.traceRequest(ctx, http.MethodPost, traceDataURL(base, path), apiKey, body)
	responseModel, usage := traceResponseFields(responseValue)
	result.ResponseModel = responseModel
	result.Usage = redactTraceMap(usage, apiKey, req.ManagementToken)
	result.Verdict["model_request"] = map[string]any{
		"status":                         response.StatusCode,
		"success":                        err == nil && response.StatusCode >= 200 && response.StatusCode < 300,
		"response_model":                 responseModel,
		"response_model_matches_request": responseModel == "" || responseModel == model,
	}
	if err != nil {
		result.ErrorClass = classifyTraceError(response.StatusCode, err.Error())
		addEvent("model_request", response.StatusCode, false, err.Error(), traceResponseDetails(response, responseValue, apiKey))
	} else {
		addEvent("model_request", response.StatusCode, response.StatusCode >= 200 && response.StatusCode < 300, "single model request completed", map[string]any{
			"request_path":     path,
			"request_model":    model,
			"response_model":   responseModel,
			"usage":            redactTraceValueWithSecrets(usage, []string{apiKey, req.ManagementToken}),
			"response_excerpt": traceExcerptWithSecret(responseValue, response.Body, apiKey),
		})
	}
	if strings.TrimSpace(req.ManagementToken) != "" && req.UpstreamKeyID != nil && *req.UpstreamKeyID > 0 {
		keyAfter, _ := s.traceReadManagementKey(ctx, base, req.ManagementToken, *req.UpstreamKeyID, apiKey)
		usageAfter, _ := s.traceReadManagementUsage(ctx, base, req.ManagementToken, *req.UpstreamKeyID, apiKey)
		observation := compareTraceBilling(keyBefore, keyAfter, usageBefore, usageAfter, model, req.ExpectedGroupName)
		if result.Billing == nil {
			result.Billing = map[string]any{}
		}
		result.Billing["key_after"] = summarizeTraceKey(keyAfter)
		result.Billing["usage_after_count"] = len(usageAfter)
		result.Billing["observation"] = observation
		addEvent("billing_after", http.StatusOK, keyAfter != nil, "read-only key and usage snapshot after probe", observation)
	}
	return result, nil
}

func (s *AccountTestService) selectTraceKeyByLogin(
	ctx context.Context,
	base, email, password, totp string,
	groupID int64,
) (string, int64, *UpstreamTraceEvent, error) {
	auth, authValue, err := s.traceRequest(
		ctx, http.MethodPost, traceAuthURL(base, "/auth/login"), "",
		[]byte(fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)),
	)
	if err != nil {
		return "", 0, &UpstreamTraceEvent{
			At: time.Now().UTC(), Phase: "login", Status: auth.StatusCode,
			Success: false, Message: "上游登录失败",
			Details: traceResponseDetails(auth, authValue, password),
		}, err
	}
	loginToken := traceStringField(authValue, "access_token")
	if loginToken == "" && strings.TrimSpace(totp) != "" {
		temp := traceStringField(authValue, "temp_token")
		if temp == "" {
			return "", 0, nil, errors.New("上游要求二次验证，但未返回临时 Token")
		}
		body := []byte(fmt.Sprintf(`{"temp_token":%q,"totp_code":%q}`, temp, totp))
		second, secondValue, secondErr := s.traceRequest(ctx, http.MethodPost, traceAuthURL(base, "/auth/login/2fa"), "", body)
		if secondErr != nil {
			return "", 0, &UpstreamTraceEvent{
				At: time.Now().UTC(), Phase: "login_2fa", Status: second.StatusCode,
				Success: false, Message: "上游二次验证失败",
				Details: traceResponseDetails(second, secondValue, totp),
			}, secondErr
		}
		loginToken = traceStringField(secondValue, "access_token")
	}
	if loginToken == "" {
		return "", 0, nil, errors.New("上游登录未返回 access_token")
	}
	response, value, err := s.traceRequest(
		ctx, http.MethodGet, traceManagementURL(base, "/keys?page=1&page_size=100&status=active"),
		loginToken, nil,
	)
	if err != nil {
		return "", 0, nil, err
	}
	var failures []string
	for _, item := range extractTraceKeyItems(value) {
		if traceInt64(item["group_id"]) != groupID {
			continue
		}
		secret, _ := item["key"].(string)
		if strings.TrimSpace(secret) == "" {
			continue
		}
		keyID := traceInt64(item["id"])
		modelsResponse, modelsValue, modelsErr := s.traceRequest(
			ctx, http.MethodGet, traceDataURL(base, "/v1/models"), secret, nil,
		)
		models := extractTraceModels(modelsValue)
		if modelsErr == nil && len(models) > 0 {
			return secret, keyID, &UpstreamTraceEvent{
				At: time.Now().UTC(), Phase: "key_selection", Status: modelsResponse.StatusCode,
				Success: true, Message: "已通过上游登录自动选择分组 Key",
				Details: map[string]any{"key_id": keyID, "group_id": groupID, "model_count": len(models)},
			}, nil
		}
		if modelsErr == nil {
			modelsErr = errors.New("模型列表为空")
		}
		failures = append(failures, fmt.Sprintf("key %d: %s", keyID, modelsErr))
	}
	message := "登录成功，但目标分组没有可用 Key"
	if len(failures) > 0 {
		message += "；" + strings.Join(failures, "；")
	}
	return "", 0, &UpstreamTraceEvent{
		At: time.Now().UTC(), Phase: "key_selection", Status: response.StatusCode,
		Success: false, Message: message,
	}, errors.New(message)
}

func (s *AccountTestService) selectTraceKey(
	ctx context.Context,
	base, managementToken string,
	groupID int64,
) (string, int64, *UpstreamTraceEvent, error) {
	response, value, err := s.traceRequest(
		ctx, http.MethodGet, traceManagementURL(base, "/keys?page=1&page_size=100"),
		managementToken, nil,
	)
	if err != nil {
		event := UpstreamTraceEvent{
			At: time.Now().UTC(), Phase: "key_selection", Status: response.StatusCode,
			Success: false, Message: "读取上游 Key 列表失败",
			Details: traceResponseDetails(response, value, managementToken),
		}
		return "", 0, &event, err
	}
	keys := extractTraceKeyItems(value)
	var failures []string
	for _, item := range keys {
		if traceInt64(item["group_id"]) != groupID {
			continue
		}
		secret, _ := item["key"].(string)
		if strings.TrimSpace(secret) == "" {
			continue
		}
		keyID := traceInt64(item["id"])
		modelsResponse, modelsValue, modelsErr := s.traceRequest(
			ctx, http.MethodGet, traceDataURL(base, "/v1/models"), secret, nil,
		)
		models := extractTraceModels(modelsValue)
		if modelsErr == nil && len(models) > 0 {
			event := UpstreamTraceEvent{
				At: time.Now().UTC(), Phase: "key_selection", Status: modelsResponse.StatusCode,
				Success: true, Message: "已自动选择可用上游 Key",
				Details: map[string]any{
					"key_id": keyID, "group_id": groupID,
					"model_count": len(models),
				},
			}
			return secret, keyID, &event, nil
		}
		if modelsErr == nil {
			modelsErr = errors.New("模型列表为空")
		}
		failures = append(failures, fmt.Sprintf("key %d: %s", keyID, modelsErr))
	}
	message := "目标分组没有可用 Key"
	if len(failures) > 0 {
		message += "；" + strings.Join(failures, "；")
	}
	return "", 0, &UpstreamTraceEvent{
		At: time.Now().UTC(), Phase: "key_selection", Status: http.StatusUnauthorized,
		Success: false, Message: message,
	}, errors.New(message)
}

func extractTraceKeyItems(value any) []map[string]any {
	var result []map[string]any
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case []any:
			for _, item := range typed {
				if object, ok := item.(map[string]any); ok {
					result = append(result, object)
				}
			}
		case map[string]any:
			for _, key := range []string{"data", "items", "keys", "list"} {
				if child, ok := typed[key]; ok {
					walk(child)
				}
			}
		}
	}
	walk(value)
	return result
}

func (s *AccountTestService) RunUpstreamTraceWithKeyGroup(
	ctx context.Context,
	accountID int64,
	req UpstreamTraceRequest,
	managementToken string,
	keyID int64,
) (*UpstreamTraceResult, error) {
	req.ManagementToken = managementToken
	req.UpstreamKeyID = &keyID
	return s.RunUpstreamTrace(ctx, accountID, req)
}

// RunUpstreamAuthorizationProbe explicitly tests POST /keys group_id handling.
// It creates a uniquely named temporary key, reads it back, and deletes it.
// The route is protected by step-up authentication because it mutates the
// upstream user's data even though cleanup is attempted automatically.
func (s *AccountTestService) RunUpstreamAuthorizationProbe(ctx context.Context, req UpstreamAuthorizationProbeRequest) (*UpstreamAuthorizationProbeResult, error) {
	if s == nil || s.httpUpstream == nil {
		return nil, errors.New("诊断服务未初始化：上游 HTTP 客户端不可用")
	}
	base, err := s.validateTraceBaseURL(req.UpstreamBaseURL)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(req.ManagementToken)
	if token == "" || req.ProbeGroupID <= 0 {
		return nil, errors.New("请填写上游管理 Token 和待检查的分组 ID")
	}
	result := &UpstreamAuthorizationProbeResult{
		TraceID:           uuid.NewString(),
		RequestedGroupID:  req.ProbeGroupID,
		AuthorizationTest: "pending",
	}
	name := "sub2api_trace_probe_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	body, _ := json.Marshal(map[string]any{"name": name, "group_id": req.ProbeGroupID})
	created, createdValue, err := s.traceRequest(ctx, http.MethodPost, traceManagementURL(base, "/keys"), token, body)
	result.CreateStatus = created.StatusCode
	if err != nil {
		result.AuthorizationTest = "inconclusive"
		result.Error = err.Error()
		return result, nil
	}
	key, ok := unwrapTraceMap(createdValue)
	if !ok {
		result.AuthorizationTest = "inconclusive"
		result.Error = "create response did not contain a key object"
		return result, nil
	}
	result.CreatedKeyID = traceInt64(key["id"])
	result.CreatedGroupID = traceInt64(key["group_id"])
	result.GroupIDApplied = result.CreatedGroupID == req.ProbeGroupID
	if result.CreatedKeyID <= 0 {
		result.AuthorizationTest = "inconclusive"
		result.Error = "create response did not contain a key id; temporary key cleanup cannot be verified"
		result.CleanupWarning = "upstream returned no temporary key id; manual cleanup review required"
		return result, nil
	}

	readback, readbackValue, readErr := s.traceRequest(
		ctx, http.MethodGet,
		traceManagementURL(base, "/keys/"+fmt.Sprint(result.CreatedKeyID)),
		token, nil,
	)
	result.ReadbackStatus = readback.StatusCode
	if readErr == nil {
		if readbackKey, ok := unwrapTraceMap(readbackValue); ok {
			result.ReadbackGroupID = traceInt64(readbackKey["group_id"])
		}
	} else {
		result.Error = readErr.Error()
	}
	result.GroupIDApplied = result.GroupIDApplied || result.ReadbackGroupID == req.ProbeGroupID

	deleted, _, deleteErr := s.traceRequest(
		ctx, http.MethodDelete,
		traceManagementURL(base, "/keys/"+fmt.Sprint(result.CreatedKeyID)),
		token, nil,
	)
	result.CleanupStatus = deleted.StatusCode
	result.CleanupOK = deleteErr == nil && deleted.StatusCode >= 200 && deleted.StatusCode < 300
	if deleteErr != nil && result.Error == "" {
		result.Error = deleteErr.Error()
	}
	if !result.CleanupOK {
		result.CleanupWarning = "temporary key deletion was not confirmed; manual cleanup review required"
	}
	if result.GroupIDApplied {
		result.AuthorizationTest = "failed"
	} else if result.CleanupOK && result.ReadbackStatus >= 200 && result.ReadbackStatus < 300 {
		result.AuthorizationTest = "passed"
	} else {
		result.AuthorizationTest = "inconclusive"
	}
	return result, nil
}

type traceHTTPResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

func (s *AccountTestService) validateTraceBaseURL(raw string) (string, error) {
	base, err := s.validateUpstreamBaseURL(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("上游地址无效：%w", err)
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("上游地址不能包含 credentials（用户名、密码、查询参数或片段）")
	}
	return strings.TrimRight(base, "/"), nil
}

func (s *AccountTestService) traceRequest(ctx context.Context, method, rawURL, token string, body []byte) (traceHTTPResponse, any, error) {
	result := traceHTTPResponse{Headers: map[string]string{}}
	requestCtx, cancel := context.WithTimeout(WithHTTPUpstreamPublicHostsOnly(WithHTTPUpstreamRedirectsDisabled(ctx)), 45*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, method, rawURL, bytes.NewReader(body))
	if err != nil {
		return result, nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "sub2api-upstream-trace/1.0")
	if token != "" && (strings.HasSuffix(request.URL.Path, "/auth/login") ||
		strings.HasSuffix(request.URL.Path, "/auth/login/2fa")) {
		request.Header.Set("Authorization", "Bearer "+token)
	} else if protocol := normalizeTraceProtocolFromPath(rawURL); protocol == "messages" {
		request.Header.Set("x-api-key", token)
		request.Header.Set("anthropic-version", "2023-06-01")
	} else if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := s.httpUpstream.Do(request, "", 0, 1)
	if err != nil {
		return result, nil, err
	}
	defer func() { _ = response.Body.Close() }()
	result.StatusCode = response.StatusCode
	for _, name := range []string{"content-type", "x-request-id", "request-id", "retry-after"} {
		if value := response.Header.Get(name); value != "" {
			result.Headers[name] = value
		}
	}
	result.Body, err = io.ReadAll(io.LimitReader(response.Body, upstreamTraceMaxResponseBytes))
	if err != nil {
		return result, nil, err
	}
	var value any
	if len(result.Body) > 0 {
		_ = json.Unmarshal(result.Body, &value)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, value, fmt.Errorf("upstream returned HTTP %d: %s", response.StatusCode, traceErrorText(value, result.Body, token))
	}
	return result, value, nil
}

func traceAuthURL(base, suffix string) string {
	parsed, _ := url.Parse(base)
	path := strings.TrimRight(parsed.Path, "/")
	for _, tail := range []string{"/v1", "/api"} {
		path = strings.TrimSuffix(path, tail)
	}
	parsed.Path = strings.TrimRight(path, "/") + "/api/v1" + suffix
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String()
}

func normalizeTraceProtocol(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "responses":
		return "responses"
	case "messages", "anthropic_messages":
		return "messages"
	default:
		return "chat_completions"
	}
}

func traceStringField(value any, key string) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if data, exists := object["data"].(map[string]any); exists {
		object = data
	}
	result, _ := object[key].(string)
	return strings.TrimSpace(result)
}

func normalizeTraceProtocolFromPath(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "chat_completions"
	}
	if strings.HasSuffix(parsed.Path, "/messages") {
		return "messages"
	}
	if strings.HasSuffix(parsed.Path, "/responses") {
		return "responses"
	}
	return "chat_completions"
}

func traceRequestBody(protocol, model, prompt string) ([]byte, string) {
	if strings.TrimSpace(prompt) == "" {
		prompt = "trace probe"
	}
	switch protocol {
	case "responses":
		body, _ := json.Marshal(map[string]any{
			"model": model, "input": prompt, "max_output_tokens": 1, "stream": false,
		})
		return body, "/v1/responses"
	case "messages":
		body, _ := json.Marshal(map[string]any{
			"model": model, "max_tokens": 1, "stream": false,
			"messages": []map[string]any{{"role": "user", "content": prompt}},
		})
		return body, "/v1/messages"
	default:
		body, _ := json.Marshal(map[string]any{
			"model": model, "max_tokens": 1, "stream": false,
			"messages": []map[string]any{{"role": "user", "content": prompt}},
		})
		return body, "/v1/chat/completions"
	}
}

func traceDataURL(base, suffix string) string {
	parsed, _ := url.Parse(base)
	path := strings.TrimRight(parsed.Path, "/")
	for _, tail := range []string{"/chat/completions", "/responses", "/messages"} {
		path = strings.TrimSuffix(path, tail)
	}
	if !strings.HasSuffix(path, "/v1") {
		path += "/v1"
	}
	suffix = strings.TrimPrefix(suffix, "/v1")
	parsed.Path = strings.TrimRight(path, "/") + suffix
	parsed.RawQuery, parsed.Fragment = "", ""
	return parsed.String()
}

func traceManagementURL(base, suffix string) string {
	parsed, _ := url.Parse(base)
	suffixURL, _ := url.Parse(suffix)
	path := strings.TrimRight(parsed.Path, "/")
	path = strings.TrimSuffix(path, "/v1")
	if !strings.HasSuffix(path, "/api") {
		path += "/api"
	}
	parsed.Path = strings.TrimRight(path, "/") + "/v1" + suffixURL.Path
	parsed.RawQuery = suffixURL.RawQuery
	parsed.Fragment = ""
	return parsed.String()
}

func unwrapTraceMap(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	if data, exists := object["data"]; exists {
		dataObject, dataOK := data.(map[string]any)
		if !dataOK {
			return nil, false
		}
		return dataObject, true
	}
	return object, true
}

func traceInt64(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		var parsed int64
		_, _ = fmt.Sscan(typed, &parsed)
		return parsed
	default:
		return 0
	}
}

func extractTraceModels(value any) []string {
	var found []string
	var walk func(any)
	walk = func(node any) {
		switch typed := node.(type) {
		case []any:
			for _, item := range typed {
				if object, ok := item.(map[string]any); ok {
					if id, ok := object["id"].(string); ok && strings.TrimSpace(id) != "" {
						found = append(found, id)
						continue
					}
				}
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					found = append(found, text)
				}
			}
		case map[string]any:
			for _, key := range []string{"data", "items", "models", "list"} {
				if child, ok := typed[key]; ok {
					walk(child)
				}
			}
		}
	}
	walk(value)
	seen := map[string]struct{}{}
	result := make([]string, 0, len(found))
	for _, model := range found {
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		result = append(result, model)
	}
	return result
}

func traceContainsModel(models []string, target string) bool {
	for _, model := range models {
		if model == target {
			return true
		}
	}
	return false
}

func traceResponseFields(value any) (string, map[string]any) {
	object, _ := value.(map[string]any)
	model, _ := object["model"].(string)
	usage, _ := object["usage"].(map[string]any)
	return model, usage
}

func traceResponseDetails(response traceHTTPResponse, value any, secret string) map[string]any {
	return map[string]any{
		"status":  response.StatusCode,
		"headers": response.Headers,
		"excerpt": traceExcerptWithSecret(value, response.Body, secret),
	}
}

func traceExcerptWithSecret(value any, raw []byte, secret string) any {
	if value != nil {
		return redactTraceValueWithSecrets(value, []string{secret})
	}
	if len(raw) > 2048 {
		raw = raw[:2048]
	}
	text := strings.TrimSpace(string(raw))
	if secret != "" {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

func traceErrorText(value any, raw []byte, secret string) string {
	var text string
	if object, ok := value.(map[string]any); ok {
		if message, ok := object["message"]; ok {
			text = fmt.Sprint(message)
		}
		if text == "" {
			if nested, ok := object["error"].(map[string]any); ok {
				if message, ok := nested["message"]; ok {
					text = fmt.Sprint(message)
				}
			}
		}
	}
	if text == "" {
		if len(raw) > 300 {
			raw = raw[:300]
		}
		text = strings.TrimSpace(string(raw))
	}
	if secret != "" {
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

func classifyTraceError(status int, message string) string {
	lower := strings.ToLower(message)
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden || strings.Contains(lower, "invalid key") || strings.Contains(lower, "unauthorized"):
		return "authentication"
	case status == http.StatusTooManyRequests || strings.Contains(lower, "rate limit"):
		return "rate_limit"
	case status == http.StatusBadRequest || status == http.StatusNotFound || status == http.StatusUnprocessableEntity ||
		strings.Contains(lower, "model_not_found") || strings.Contains(lower, "unknown provider") ||
		strings.Contains(lower, "not supported"):
		return "model_not_supported"
	case status == http.StatusPaymentRequired || strings.Contains(lower, "quota") || strings.Contains(lower, "balance") || strings.Contains(lower, "insufficient"):
		return "quota"
	case status >= 500 || strings.Contains(lower, "timeout") || strings.Contains(lower, "connection"):
		return "temporary_upstream"
	default:
		return "unknown"
	}
}

func redactTraceValueWithSecrets(value any, secrets []string) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "token") || strings.Contains(lower, "secret") ||
				strings.Contains(lower, "password") || lower == "key" || strings.Contains(lower, "cookie") {
				result[key] = "[REDACTED]"
				continue
			}
			result[key] = redactTraceValueWithSecrets(child, secrets)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, child := range typed {
			result[index] = redactTraceValueWithSecrets(child, secrets)
		}
		return result
	case string:
		text := typed
		for _, secret := range secrets {
			if secret != "" {
				text = strings.ReplaceAll(text, secret, "[REDACTED]")
			}
		}
		return text
	default:
		return value
	}
}

func redactTraceMap(value map[string]any, secrets ...string) map[string]any {
	if value == nil {
		return nil
	}
	redacted, _ := redactTraceValueWithSecrets(value, secrets).(map[string]any)
	return redacted
}

func (s *AccountTestService) traceReadManagementKey(ctx context.Context, base, token string, id int64, apiKey string) (map[string]any, error) {
	response, value, err := s.traceRequest(ctx, http.MethodGet, traceManagementURL(base, "/keys/"+strconv.FormatInt(id, 10)), token, nil)
	if err != nil {
		return nil, err
	}
	if key, ok := unwrapTraceMap(value); ok {
		return key, nil
	}
	return nil, fmt.Errorf("key lookup returned HTTP %d with invalid response", response.StatusCode)
}

func (s *AccountTestService) traceReadManagementUsage(ctx context.Context, base, token string, id int64, apiKey string) ([]map[string]any, error) {
	response, value, err := s.traceRequest(ctx, http.MethodGet, traceManagementURL(base, "/usage?page=1&page_size=100"), token, nil)
	if err != nil {
		return nil, err
	}
	data, ok := unwrapTraceMap(value)
	if !ok {
		return nil, fmt.Errorf("usage lookup returned HTTP %d with invalid response", response.StatusCode)
	}
	items, _ := data["items"].([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, _ := item.(map[string]any)
		if traceInt64(row["api_key_id"]) == id {
			result = append(result, row)
		}
	}
	return result, nil
}

func summarizeTraceKey(key map[string]any) map[string]any {
	if key == nil {
		return nil
	}
	groupID := traceInt64(key["group_id"])
	groupName := ""
	if group, ok := key["group"].(map[string]any); ok {
		if groupID == 0 {
			groupID = traceInt64(group["id"])
		}
		groupName, _ = group["name"].(string)
	}
	if name, ok := key["group_name"].(string); ok && groupName == "" {
		groupName = name
	}
	return map[string]any{
		"id":         traceInt64(key["id"]),
		"group_id":   groupID,
		"group_name": groupName,
		"status":     key["status"],
	}
}

func compareTraceBilling(before, after map[string]any, beforeRows, afterRows []map[string]any, requestModel, expectedGroup string) map[string]any {
	result := map[string]any{
		"request_model":           requestModel,
		"usage_rows_before":       len(beforeRows),
		"usage_rows_after":        len(afterRows),
		"new_usage_rows":          len(afterRows) - len(beforeRows),
		"accounting_confirmation": "unavailable",
	}
	beforeSummary := summarizeTraceKey(before)
	afterSummary := summarizeTraceKey(after)
	result["key_group_before"] = beforeSummary
	result["key_group_after"] = afterSummary
	if afterSummary != nil {
		if groupName, ok := afterSummary["group_name"].(string); ok && expectedGroup != "" {
			result["expected_group_name"] = expectedGroup
			result["expected_group_match"] = strings.EqualFold(strings.TrimSpace(groupName), strings.TrimSpace(expectedGroup))
		}
	}
	beforeIDs := map[int64]struct{}{}
	for _, row := range beforeRows {
		beforeIDs[traceInt64(row["id"])] = struct{}{}
	}
	for _, row := range afterRows {
		if _, exists := beforeIDs[traceInt64(row["id"])]; exists {
			continue
		}
		safe := map[string]any{}
		for _, key := range []string{"id", "model", "group_id", "rate_multiplier", "actual_cost", "total_cost", "created_at", "request_type"} {
			if value, exists := row[key]; exists {
				safe[key] = value
			}
		}
		result["new_usage_row"] = safe
		billedModel, _ := row["model"].(string)
		result["billed_model"] = billedModel
		result["billed_model_matches_request"] = billedModel == requestModel
		result["accounting_confirmation"] = "usage_row_observed"
		break
	}
	return result
}
