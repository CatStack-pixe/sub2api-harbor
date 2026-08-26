-- Add the upstream CN providers without excluding existing fork route targets.
ALTER TABLE composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN (
        'anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'agnes',
        'deepseek', 'nvidia', 'tokenrhythm', 'kimi', 'zhipu', 'chatanywhere', 'glm'
    ));
