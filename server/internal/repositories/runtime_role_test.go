package repositories

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestTenantRuntimeRole(t *testing.T) {
	for _, tc := range []struct {
		name   string
		env    string
		unsafe bool
	}{
		{"runtime", "TEST_TENANT_DB_DSN", false},
		{"owner", "TEST_DB_DSN", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if dsn == "" {
				t.Skip(tc.env + " tidak diisi")
			}
			db, err := sql.Open("pgx", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			unsafe, err := TenantRoleUnsafe(context.Background(), db)
			if err != nil || unsafe != tc.unsafe {
				t.Fatalf("unsafe=%v err=%v", unsafe, err)
			}
		})
	}
}
