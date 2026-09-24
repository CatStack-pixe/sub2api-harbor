-- Tierflow is an API-key OpenAI-compatible relay. Keep every existing fork
-- platform while adding it to quotas, composite routes and channel monitors.
-- No existing migration or account/group configuration is rewritten.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'agnes', 'deepseek', 'nvidia', 'tokenrhythm', 'kimi', 'zhipu', 'chatanywhere', 'glm', 'modelscope', 'dashscope', 'minimax', 'volcengine', 'sensenova', 'opencode_go', 'tierflow'));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'agnes', 'deepseek', 'nvidia', 'tokenrhythm', 'kimi', 'zhipu', 'chatanywhere', 'glm', 'modelscope', 'dashscope', 'minimax', 'volcengine', 'sensenova', 'opencode_go', 'tierflow'));

ALTER TABLE channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;

ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity', 'kimi', 'zhipu', 'deepseek', 'sensenova', 'minimax', 'opencode_go', 'tierflow'));

ALTER TABLE channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;

ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity', 'kimi', 'zhipu', 'deepseek', 'sensenova', 'minimax', 'opencode_go', 'tierflow'));
