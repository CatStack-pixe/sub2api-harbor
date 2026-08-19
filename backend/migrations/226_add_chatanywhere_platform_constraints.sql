-- Keep database platform constraints aligned with the application platform lists.
-- Earlier constraints predate several supported providers; ChatAnywhere must be
-- accepted by both user quota rows and composite model route targets.
ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN (
        'anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'agnes',
        'deepseek', 'nvidia', 'tokenrhythm', 'kimi', 'chatanywhere'
    ));

ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN (
        'anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'agnes',
        'deepseek', 'nvidia', 'tokenrhythm', 'kimi', 'chatanywhere'
    ));
