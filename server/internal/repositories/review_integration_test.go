//go:build integration

package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"hsg/internal/harness/review"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
)

// Requires a migrated, non-superuser database (RLS must actually apply).
func TestReviewRepositoryIntegration(t *testing.T) {
	dsn := os.Getenv("HSG_REVIEW_TEST_DSN")
	if dsn == "" {
		t.Skip("set HSG_REVIEW_TEST_DSN to an isolated migrated database")
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
	tenantA := newTenant("review-a")
	tenantB := newTenant("review-b")

	r := NewReviewRepository(db)
	proposed := func() review.Item {
		return review.Item{
			RunID:   "run-" + uuid.New().String(),
			Subject: "claim-1",
			Result:  json.RawMessage(`{"verdict":"accept"}`),
			Reason:  "low confidence",
			Role:    "reviewer",
		}
	}

	ctxA := lib.WithTenant(ctx, tenantA)
	created, err := r.Create(ctxA, proposed())
	if err != nil {
		t.Fatal(err)
	}
	// A re-run of the same (run, subject) replaces the open item, not duplicates.
	if _, err = r.Create(ctxA, created); err != nil {
		t.Fatalf("idempotent create: %v", err)
	}

	// Tenant B creates its own item; it must be invisible from tenant A.
	ctxB := lib.WithTenant(ctx, tenantB)
	other, err := r.Create(ctxB, proposed())
	if err != nil {
		t.Fatal(err)
	}

	// Tenant A decides its own item; the decision history is kept.
	decided, err := r.Decide(ctxA, created.ID, review.Decision{
		Reviewer: "r1", Role: "tenant_admin", Result: json.RawMessage(`{"verdict":"reject"}`),
	})
	if err != nil {
		t.Fatalf("tenant A must decide its own item: %v", err)
	}
	if len(decided.Decisions) != 1 || decided.Status != review.StatusDecided {
		t.Fatalf("decision = %+v", decided)
	}

	// Cross-tenant: B cannot read or decide A's item. RLS hides A's row, so
	// the store reports it as not found rather than a permission error.
	if _, err = r.Get(ctxB, created.ID); !errors.Is(err, review.ErrNotFound) {
		t.Fatalf("cross-tenant get err = %v; want not found", err)
	}
	if _, err = r.Decide(ctxB, created.ID, review.Decision{
		Reviewer: "r2", Role: "tenant_admin", Result: json.RawMessage(`{"verdict":"accept"}`),
	}); !errors.Is(err, review.ErrNotFound) {
		t.Fatalf("cross-tenant decide err = %v; want not found", err)
	}

	// A tenantless context is forbidden.
	if _, err = r.Create(ctx, proposed()); err != apperr.ErrForbidden {
		t.Fatalf("tenantless create err = %v; want forbidden", err)
	}

	// B's item is still open and visible to B itself.
	got, err := r.Get(ctxB, other.ID)
	if err != nil || got.Status != review.StatusOpen {
		t.Fatalf("get own item = %+v err = %v", got, err)
	}
}
