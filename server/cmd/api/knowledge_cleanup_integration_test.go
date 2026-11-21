package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"hsg/internal/lib/blob"
	"hsg/internal/lib/kb"
	"hsg/internal/repositories"
)

// Simulate a lost COMMIT acknowledgement after PostgreSQL committed. This
// exercises the repository's actual error path without unreliable timing.
type cleanupLostAckConnector struct {
	driver.Connector
	lose *atomic.Bool
}

func (c cleanupLostAckConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return cleanupLostAckConn{Conn: conn, lose: c.lose}, nil
}

type cleanupLostAckConn struct {
	driver.Conn
	lose *atomic.Bool
}

func (c cleanupLostAckConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c cleanupLostAckConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return cleanupLostAckTx{Tx: tx, lose: c.lose}, nil
}

type cleanupLostAckTx struct {
	driver.Tx
	lose *atomic.Bool
}

func (tx cleanupLostAckTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.lose.Swap(false) {
		return errors.New("injected lost commit acknowledgement")
	}
	return nil
}

type cleanupFailingStore struct {
	blob.Store
	fail bool
}

func (s *cleanupFailingStore) Delete(ctx context.Context, key string) error {
	if s.fail {
		return errors.New("injected storage outage")
	}
	return s.Store.Delete(ctx, key)
}

func TestKnowledgeCleanupIntegration(t *testing.T) {
	owner, dsn := chatTestDB(t, "KNOWLEDGE_CLEANUP_TEST_DB_DSN")
	ctx := context.Background()
	tenantDB := tenantTestPool(t, owner, dsn)
	var role string
	chatRequire(t, tenantDB.QueryRow(`SELECT current_user`).Scan(&role))
	chatExec(t, owner, fmt.Sprintf(`GRANT SELECT,INSERT,DELETE ON knowledge_documents,knowledge_chunks TO %s`, role))
	for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
		var allowed bool
		chatRequire(t, owner.QueryRow(`SELECT has_table_privilege($1,'public.knowledge_blob_cleanup',$2)`, role, privilege).Scan(&allowed))
		if allowed {
			t.Fatalf("runtime queue grant: %s", privilege)
		}
	}
	var execute bool
	chatRequire(t, owner.QueryRow(`SELECT has_function_privilege($1,'public.enqueue_knowledge_blob_cleanup()','EXECUTE')`, role).Scan(&execute))
	if execute {
		t.Fatal("runtime may invoke trigger function directly")
	}
	actor := uuid.New()
	chatExec(t, owner, `INSERT INTO users(id,name,email,role) VALUES($1,'Cleanup',$2,'super_user')`, actor, actor.String()+"@test.invalid")
	disk, err := blob.NewDisk(t.TempDir())
	chatRequire(t, err)
	defer disk.Close()
	store := &cleanupFailingStore{Store: disk}
	queue := repositories.NewKnowledgeCleanupRepository(owner)
	repo := repositories.NewKnowledgeRepository(tenantDB)
	put := func(t *testing.T) (uuid.UUID, string) {
		t.Helper()
		id := uuid.New()
		key := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
		chatRequire(t, disk.Put(ctx, key, []byte("synthetic fixture")))
		chatExec(t, owner, `INSERT INTO knowledge_documents(id,title,filename,format,size_bytes,sha256,storage_key,uploaded_by) VALUES($1,'Cleanup','fixture.txt','txt',17,$2,$2,$3)`, id, key, actor)
		return id, key
	}
	assertQueued := func(t *testing.T, key string, want bool) {
		t.Helper()
		var exists bool
		chatRequire(t, owner.QueryRow(`SELECT EXISTS(SELECT 1 FROM public.knowledge_blob_cleanup WHERE storage_key=$1)`, key).Scan(&exists))
		if exists != want {
			t.Fatalf("queued=%v want=%v", exists, want)
		}
	}
	t.Run("rollback_is_atomic", func(t *testing.T) {
		id, key := put(t)
		tx, err := owner.BeginTx(ctx, nil)
		chatRequire(t, err)
		defer tx.Rollback()
		_, err = tx.Exec(`DELETE FROM knowledge_documents WHERE id=$1`, id)
		chatRequire(t, err)
		var n int
		chatRequire(t, tx.QueryRow(`SELECT count(*) FROM knowledge_blob_cleanup WHERE storage_key=$1`, key).Scan(&n))
		if n != 1 {
			t.Fatal("delete did not enqueue in transaction")
		}
		assertQueued(t, key, false)
		chatRequire(t, tx.Rollback())
		assertQueued(t, key, false)
		chatRequire(t, owner.QueryRow(`SELECT count(*) FROM knowledge_documents WHERE id=$1`, id).Scan(&n))
		if n != 1 {
			t.Fatal("rollback lost metadata")
		}
	})
	t.Run("storage_failure_then_restart_retry", func(t *testing.T) {
		id, key := put(t)
		got, err := repo.Delete(ctx, actor, nil, id)
		chatRequire(t, err)
		if got != key {
			t.Fatal("wrong key")
		}
		assertQueued(t, key, true)
		store.fail = true
		if kb.CleanupOnce(ctx, queue, store) == nil {
			t.Fatal("ignored storage outage")
		}
		assertQueued(t, key, true)
		store.fail = false
		// A new repository has no in-memory state from the failed attempt.
		chatRequire(t, kb.CleanupOnce(ctx, repositories.NewKnowledgeCleanupRepository(owner), store))
		assertQueued(t, key, false)
		_, err = disk.Get(ctx, key)
		if !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("blob survived: %v", err)
		}
	})
	t.Run("immediate_best_effort_already_deleted", func(t *testing.T) {
		id, key := put(t)
		_, err := repo.Delete(ctx, actor, nil, id)
		chatRequire(t, err)
		chatRequire(t, disk.Delete(ctx, key))
		chatRequire(t, kb.CleanupOnce(ctx, queue, store))
		assertQueued(t, key, false)
	})
	t.Run("commit_unknown_still_queued", func(t *testing.T) {
		id, key := put(t)
		cfg, err := pgx.ParseConfig(dsn)
		chatRequire(t, err)
		cfg.RuntimeParams["role"] = role
		var lose atomic.Bool
		pool := sql.OpenDB(cleanupLostAckConnector{Connector: stdlib.GetConnector(*cfg), lose: &lose})
		defer pool.Close()
		uncertain := repositories.NewKnowledgeRepository(pool)
		lose.Store(true)
		if _, err := uncertain.Delete(ctx, actor, nil, id); err == nil || err.Error() != "injected lost commit acknowledgement" {
			t.Fatalf("expected lost commit acknowledgement, got %v", err)
		}
		assertQueued(t, key, true)
		var exists bool
		chatRequire(t, owner.QueryRow(`SELECT EXISTS(SELECT 1 FROM knowledge_documents WHERE id=$1)`, id).Scan(&exists))
		if exists {
			t.Fatal("delete did not actually commit")
		}
		chatRequire(t, kb.CleanupOnce(ctx, queue, store))
		assertQueued(t, key, false)
		_, err = disk.Get(ctx, key)
		if !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("blob survived unknown commit: %v", err)
		}
	})
	t.Run("tenant_cascade_enqueues", func(t *testing.T) {
		tenant := uuid.New()
		chatExec(t, owner, `INSERT INTO tenants(id,name) VALUES($1,'Cleanup')`, tenant)
		key := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
		chatExec(t, owner, `INSERT INTO knowledge_documents(tenant_id,title,filename,format,size_bytes,sha256,storage_key) VALUES($1,'Cleanup','fixture.txt','txt',1,$2,$2)`, tenant, key)
		chatExec(t, owner, `DELETE FROM tenants WHERE id=$1`, tenant)
		assertQueued(t, key, true)
		chatRequire(t, kb.CleanupOnce(ctx, queue, store))
		assertQueued(t, key, false)
	})
}
