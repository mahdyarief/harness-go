//go:build integration

package repositories

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"hsg/internal/harness/services"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
)

// Requires a migrated, non-superuser database (RLS must actually apply).
func TestRunLogRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("HSG_RUNLOG_TEST_DSN")
	if dsn == "" {
		t.Skip("set HSG_RUNLOG_TEST_DSN to an isolated migrated database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	if unsafe, err := TenantRoleUnsafe(ctx, db); err != nil {
		t.Fatal(err)
	} else if unsafe {
		t.Fatal("test database role must not bypass RLS")
	}

	newTenant := func(name string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, id.String()); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ($1,$2)`, id, name); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return id
	}
	tenantA := newTenant("runlog-a")
	tenantB := newTenant("runlog-b")

	r := NewRunLogRepository(db)
	runID := uuid.New().String()
	if err = r.CreateRun(ctx, services.RunRecord{
		ID: runID, TenantID: tenantA.String(), ProfileName: "worker", Status: "running",
	}); err != nil {
		t.Fatal(err)
	}

	ctxA := lib.WithTenant(ctx, tenantA)
	event := services.RunLogEvent{RunID: runID, Member: "worker", Type: "run/start", Payload: []byte(`{}`)}
	if err = r.AppendEvent(ctxA, event); err != nil {
		t.Fatal(err)
	}
	event.Type = "model/call"
	event.Payload = []byte(`{"prompt_tokens":7,"completion_tokens":2}`)
	if err = r.AppendEvent(ctxA, event); err != nil {
		t.Fatal(err)
	}
	if err = r.FinishRun(ctxA, runID, services.RunCompletion{Status: "ok", PromptTokens: 7, CompletionTokens: 2}); err != nil {
		t.Fatal(err)
	}

	events, err := r.Events(ctxA, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("events = %+v", events)
	}

	// The append-only trigger rejects every mutation, statement-wide.
	if _, err = db.ExecContext(ctx, `UPDATE run_events SET type = 'x' WHERE run_id = $1`, runID); err == nil {
		t.Fatal("UPDATE on run_events accepted")
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM run_events WHERE run_id = $1`, runID); err == nil {
		t.Fatal("DELETE on run_events accepted")
	}

	// RLS: tenant B neither reads nor appends to tenant A's run.
	ctxB := lib.WithTenant(ctx, tenantB)
	if _, err = r.Events(ctxB, runID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("cross-tenant read err = %v; want ErrNotFound", err)
	}
	if err = r.AppendEvent(ctxB, services.RunLogEvent{RunID: runID, Type: "run/end"}); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("cross-tenant append err = %v; want ErrNotFound", err)
	}
	if err = r.FinishRun(ctxB, runID, services.RunCompletion{Status: "x"}); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("cross-tenant finish err = %v; want ErrNotFound", err)
	}

	// Without a tenant in the context nothing is reachable.
	if _, err = r.Events(ctx, runID); !errors.Is(err, apperr.ErrForbidden) {
		t.Fatalf("tenantless read err = %v; want ErrForbidden", err)
	}
}
