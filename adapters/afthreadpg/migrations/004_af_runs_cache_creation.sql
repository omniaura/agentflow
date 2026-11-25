ALTER TABLE af_runs
    ADD COLUMN cache_creation_input_tokens bigint NOT NULL DEFAULT 0;
