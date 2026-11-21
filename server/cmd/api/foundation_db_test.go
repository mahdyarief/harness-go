package main

import (
	"os"
	"testing"
)

func TestConnectTenantDB(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     string
		allowed bool
	}{
		{"runtime", "TEST_TENANT_DB_DSN", true},
		{"owner", "TEST_DB_DSN", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if dsn == "" {
				t.Skip(tc.env + " tidak diisi")
			}
			db, err := connectTenantDB(dsn)
			if db != nil {
				defer db.Close()
			}
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
}
