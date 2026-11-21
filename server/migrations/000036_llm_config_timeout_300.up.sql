UPDATE llm_config SET timeout_seconds = 300 WHERE singleton = TRUE AND timeout_seconds < 300;
