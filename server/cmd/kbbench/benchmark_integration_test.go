package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestDisposableBenchmark(t *testing.T) {
	adminDSN, runtimeDSN := os.Getenv("TEST_KBEVAL_ADMIN_DSN"), os.Getenv("TEST_KBEVAL_RUNTIME_DSN")
	if adminDSN == "" || runtimeDSN == "" {
		t.Skip("requires disposable local kbeval database")
	}
	open := func(dsn string) *sql.DB {
		cfg, err := pgx.ParseConfig(dsn)
		if err != nil {
			t.Fatal("invalid DSN")
		}
		ip := net.ParseIP(cfg.Host)
		if ip == nil || !ip.IsLoopback() || cfg.Database != "kbeval" || len(cfg.Fallbacks) != 0 {
			t.Fatal("refusing non-disposable database")
		}
		db := stdlib.OpenDB(*cfg)
		t.Cleanup(func() { db.Close() })
		return db
	}
	admin := open(adminDSN)
	_ = open(runtimeDSN) // Validate before any fixture writes.
	actor, document := uuid.New(), uuid.New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() {
		admin.Exec(`DELETE FROM knowledge_documents WHERE id=$1`, document)
		admin.Exec(`DELETE FROM users WHERE id=$1`, actor)
	})
	if _, err := admin.ExecContext(ctx, `INSERT INTO users(id,name,email,role) VALUES($1,'bench synthetic','bench@example.invalid','super_user')`, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO knowledge_documents(id,title,filename,format,size_bytes,sha256,storage_key,uploaded_by) VALUES($1,'Synthetic','fixture.txt','txt',1,repeat('b',64),repeat('b',64),$2)`, document, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO knowledge_chunks(document_id,position,content) VALUES($1,0,'benchmarksecret')`, document); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "queries.json")
	if err := os.WriteFile(path, []byte(`["benchmarksecret","absentterm"]`), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--dsn", runtimeDSN, "--actor", actor.String(), "--queries", path, "--concurrency", "3", "--iterations", "10"}
	var out bytes.Buffer
	if code := execute(ctx, args, &out); code != 0 {
		t.Fatalf("code=%d %s", code, out.String())
	}
	var report benchmarkReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Attempts != 20 || report.Successes != 20 || report.Errors != 0 || report.Hits != 10 {
		t.Fatalf("%+v", report)
	}
	if strings.Contains(out.String(), "benchmarksecret") || strings.Contains(out.String(), runtimeDSN) {
		t.Fatal("private input leaked")
	}
	t.Logf("measured disposable benchmark %s", strings.TrimSpace(out.String()))
	tenant := uuid.New()
	t.Cleanup(func() {
		admin.Exec(`DELETE FROM tenant_memberships WHERE tenant_id=$1`, tenant)
		admin.Exec(`DELETE FROM tenants WHERE id=$1`, tenant)
	})
	if _, err := admin.ExecContext(ctx, `INSERT INTO tenants(id,name) VALUES($1,'synthetic bench')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `INSERT INTO tenant_memberships(tenant_id,user_id,role) VALUES($1,$2,'reviewer')`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `UPDATE users SET role='user' WHERE id=$1`, actor); err != nil {
		t.Fatal(err)
	}
	tenantArgs := append(append([]string{}, args...), "--tenant", tenant.String())
	out.Reset()
	if code := execute(ctx, tenantArgs, &out); code != 0 {
		t.Fatalf("tenant code=%d %s", code, out.String())
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Successes != 20 || report.Hits != 10 || report.Scope != "tenant-personal" {
		t.Fatalf("tenant %+v", report)
	}
	if _, err := admin.ExecContext(ctx, `DELETE FROM tenant_memberships WHERE tenant_id=$1 AND user_id=$2`, tenant, actor); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := execute(ctx, tenantArgs, &out); code == 0 {
		t.Fatal("revoked membership allowed")
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Errors != 20 || report.Hits != 0 {
		t.Fatalf("revoked %+v", report)
	}
}
