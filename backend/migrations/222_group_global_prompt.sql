ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS global_prompt_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS global_prompt TEXT NOT NULL DEFAULT '';

UPDATE groups
SET global_prompt_enabled = FALSE,
    global_prompt = ''
WHERE global_prompt_enabled IS NULL
   OR global_prompt IS NULL;
