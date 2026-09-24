-- SenseAudio is an API-key OpenAI-compatible provider. Preserve all existing
-- quota and composite-route platforms while allowing dedicated SenseAudio groups.

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'agnes', 'deepseek', 'nvidia', 'tokenrhythm', 'kimi', 'zhipu', 'chatanywhere', 'glm', 'modelscope', 'dashscope', 'minimax', 'volcengine', 'sensenova', 'opencode_go', 'tierflow', 'senseaudio'));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'agnes', 'deepseek', 'nvidia', 'tokenrhythm', 'kimi', 'zhipu', 'chatanywhere', 'glm', 'modelscope', 'dashscope', 'minimax', 'volcengine', 'sensenova', 'opencode_go', 'tierflow', 'senseaudio'));
