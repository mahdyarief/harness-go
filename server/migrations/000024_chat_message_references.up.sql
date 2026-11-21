ALTER TABLE ai_chat_messages ADD COLUMN "references" jsonb NOT NULL DEFAULT '[]'::jsonb;
