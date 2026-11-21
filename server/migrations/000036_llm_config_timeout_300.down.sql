UPDATE llm_config SET timeout_seconds = 120 WHERE singleton = TRUE AND timeout_seconds >= 300;
