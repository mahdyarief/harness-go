package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"hsg/internal/lib"
)

// This uses a second disposable DB because failed down migrations mark it dirty.
func TestPlatformChatMigrationIntegration(t *testing.T) {
	db, _ := chatTestDB(t, "PLATFORM_CHAT_MIGRATION_TEST_DB_DSN")
	m, err := lib.NewMigrator(db)
	chatRequire(t, err)
	chatRequire(t, m.Steps(-1)) // 15 -> 14
	user, session, tenant, tenantSession := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	chatExec(t, db, `INSERT INTO users(id,name,email,role) VALUES($1,'Migration',$2,'super_user')`, user, user.String()+"@test.invalid")
	chatExec(t, db, `INSERT INTO tenants(id,name) VALUES($1,'Migration')`, tenant)
	chatExec(t, db, `INSERT INTO ai_chat_sessions(id,tenant_id,user_id,title) VALUES($1,$2,$3,'tenant retained')`, tenantSession, tenant, user)
	chatExec(t, db, `INSERT INTO ai_chat_sessions(id,user_id,title) VALUES($1,$2,'platform retained')`, session, user)
	blocked := func(t *testing.T) {
		t.Helper()
		err := m.Steps(-1)
		if err == nil || !strings.Contains(err.Error(), "platform chat data must be archived before rollback") {
			t.Fatalf("unsafe down error = %v", err)
		}
		version, dirty, err := m.Version()
		chatRequire(t, err)
		if version != 10 || !dirty {
			t.Fatalf("failed migration state = %d dirty=%v", version, dirty)
		}
		var ready bool
		chatRequire(t, db.QueryRow(`SELECT to_regclass('public.platform_chat_token_usages') IS NOT NULL AND EXISTS(SELECT 1 FROM pg_policies WHERE tablename='ai_chat_sessions' AND policyname='session_personal') AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='ai_chat_sessions' AND column_name='tenant_id' AND is_nullable='YES')`).Scan(&ready))
		if !ready {
			t.Fatal("blocked rollback partially changed platform schema")
		}
		// The guard ran before any DDL; only now repair metadata in this disposable DB.
		chatRequire(t, m.Force(14))
	}
	t.Run("platform_session_blocks_down", blocked)
	var title string
	chatRequire(t, db.QueryRow(`SELECT title FROM ai_chat_sessions WHERE id=$1`, session).Scan(&title))
	if title != "platform retained" {
		t.Fatal("blocked rollback lost session")
	}
	chatExec(t, db, `DELETE FROM ai_chat_sessions WHERE id=$1`, session)
	chatExec(t, db, `INSERT INTO platform_chat_token_usages(user_id,session_id,prompt_tokens,completion_tokens,total_tokens,failed) VALUES($1,$2,1,2,3,true)`, user, session)
	t.Run("ledger_without_session_blocks_down", blocked)
	var total int
	chatRequire(t, db.QueryRow(`SELECT total_tokens FROM platform_chat_token_usages WHERE session_id=$1`, session).Scan(&total))
	if total != 3 {
		t.Fatal("blocked rollback lost metering")
	}
	// Archival cleanup is intentionally superuser-only on this disposable DB.
	_, err = db.Exec(`TRUNCATE platform_chat_token_usages`)
	chatSQLState(t, err, "P0001")
	// Explicit simulated archival, disabling immutability only inside this transaction.
	tx, err := db.Begin()
	chatRequire(t, err)
	defer tx.Rollback()
	for _, query := range []string{`ALTER TABLE platform_chat_token_usages DISABLE TRIGGER platform_usage_immutable`, `TRUNCATE platform_chat_token_usages`, `ALTER TABLE platform_chat_token_usages ENABLE TRIGGER platform_usage_immutable`, `DELETE FROM ai_chat_sessions WHERE tenant_id IS NULL`} {
		_, err = tx.Exec(query)
		chatRequire(t, err)
	}
	chatRequire(t, tx.Commit())
	chatRequire(t, m.Steps(-1)) // 14 -> 10 (versions 11-13 do not exist)
	version, dirty, err := m.Version()
	chatRequire(t, err)
	if version != 10 || dirty {
		t.Fatalf("clean down = %d dirty=%v", version, dirty)
	}
	var reverted bool
	chatRequire(t, db.QueryRow(`SELECT to_regclass('public.platform_chat_token_usages') IS NULL AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='ai_chat_sessions' AND column_name='tenant_id' AND is_nullable='NO')`).Scan(&reverted))
	if !reverted {
		t.Fatal("down did not restore tenant-only schema")
	}
	chatRequire(t, db.QueryRow(`SELECT title FROM ai_chat_sessions WHERE id=$1`, tenantSession).Scan(&title))
	if title != "tenant retained" {
		t.Fatal("down lost tenant session")
	}
	chatRequire(t, m.Up()) // 10 -> 15
	version, dirty, err = m.Version()
	chatRequire(t, err)
	if version != 15 || dirty {
		t.Fatalf("re-up = %d dirty=%v", version, dirty)
	}
	chatRequire(t, db.QueryRow(`SELECT title FROM ai_chat_sessions WHERE id=$1`, tenantSession).Scan(&title))
	if title != "tenant retained" {
		t.Fatal("re-up lost tenant session")
	}
	chatExec(t, db, `INSERT INTO ai_chat_sessions(id,user_id,title) VALUES($1,$2,'re-up works')`, uuid.New(), user)
	var limits bool
	chatRequire(t, db.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='llm_config_max_tokens_check' AND pg_get_constraintdef(oid) LIKE '%16384%') AND EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='llm_config' AND column_name='max_tokens' AND column_default LIKE '%16384%')`).Scan(&limits))
	if !limits {
		t.Fatal("migration 15 not restored")
	}
}
