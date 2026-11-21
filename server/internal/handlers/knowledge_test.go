package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
	"hsg/internal/lib/kb"
	"hsg/internal/middlewares"
)

func TestKnowledgeScope(t *testing.T) {
	uid, tenant := uuid.New(), uuid.New()
	for _, tc := range []struct {
		path       string
		signed     bool
		status     int
		wantTenant bool
	}{
		{"/platform", false, 401, false}, {"/platform", true, 200, false},
		{"/tenants/" + tenant.String(), true, 200, true}, {"/tenants/invalid", true, 400, false},
		{"/tenants/" + uuid.Nil.String(), true, 400, false},
	} {
		t.Run(tc.path+"_"+httpStatusName(tc.signed), func(t *testing.T) {
			server := fiber.New(fiber.Config{ErrorHandler: middlewares.ErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))})
			handler := func(c fiber.Ctx) error {
				if tc.signed {
					lib.ContextSetUID(c, uid)
				}
				actor, scope, err := knowledgeScope(c)
				if err != nil {
					return err
				}
				if actor != uid || (scope != nil) != tc.wantTenant {
					t.Fatal("scope salah")
				}
				if scope != nil && *scope != tenant {
					t.Fatal("tenant berubah")
				}
				return c.SendStatus(200)
			}
			server.Get("/platform", handler)
			server.Get("/tenants/:tenant_id", handler)
			response, err := server.Test(httptest.NewRequest("GET", tc.path, nil))
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status=%d", response.StatusCode)
			}
		})
	}
}

func TestKnowledgeTenantHeader(t *testing.T) {
	uid, tenant := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name   string
		header string
		status int
	}{
		{"absent", "", 200},
		{"matching", tenant.String(), 200},
		{"mismatch", uuid.NewString(), 400},
		{"invalid", "invalid", 400},
		{"nil", uuid.Nil.String(), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := fiber.New(fiber.Config{ErrorHandler: middlewares.ErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))})
			server.Get("/tenants/:tenant_id", func(c fiber.Ctx) error {
				lib.ContextSetUID(c, uid)
				_, scope, err := knowledgeScope(c)
				if err != nil {
					return err
				}
				if scope == nil || *scope != tenant {
					t.Fatal("scope tenant berubah")
				}
				return c.SendStatus(200)
			})
			request := httptest.NewRequest("GET", "/tenants/"+tenant.String(), nil)
			if tc.header != "" {
				request.Header.Set("X-Tenant-ID", tc.header)
			}
			response, err := server.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status=%d, want=%d", response.StatusCode, tc.status)
			}
		})
	}
}

func httpStatusName(signed bool) string {
	if signed {
		return "signed"
	}
	return "anonymous"
}

func TestKnowledgeFileErrors(t *testing.T) {
	for _, err := range []error{kb.ErrEmpty, kb.ErrCorrupt, kb.ErrTooLarge, kb.ErrUnsupportedFormat} {
		status, _ := apperr.From(knowledgeFileError(err))
		if status != 400 {
			t.Fatalf("%v: %d", err, status)
		}
	}
	if !errors.Is(knowledgeFileError(context.Canceled), context.Canceled) {
		t.Fatal("cancellation hilang")
	}
}
