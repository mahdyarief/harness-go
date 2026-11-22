package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"hsg/internal/harness/review"
	"hsg/internal/lib"
	"hsg/internal/middlewares"
	"log/slog"
)

const (
	reviewTenant = "11111111-1111-1111-1111-111111111111"
	reviewUser   = "4f54815f-a2d7-4610-b0dd-024b86491885"
	reviewSchema = `{"type":"object","properties":{"amount":{"type":"number"}},"required":["amount"]}`
)

func reviewHarness(t *testing.T, role string) (*reviewHandler, *review.Memory, *fiber.App) {
	t.Helper()
	store := review.NewMemory()
	h := &reviewHandler{
		store: store,
		role: func(fiber.Ctx, string, string) (string, error) {
			return role, nil
		},
	}
	server := fiber.New(fiber.Config{ErrorHandler: middlewares.ErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil)))})
	server.Use(func(c fiber.Ctx) error {
		lib.ContextSetUID(c, uuid.MustParse(reviewUser))
		lib.WithTenant(c.Context(), uuid.MustParse(reviewTenant))
		return c.Next()
	})
	group := server.Group("/tenants/:tenant_id/reviews")
	group.Get("/", h.List)
	group.Get("/:id", h.Get)
	group.Post("/:id/decisions", h.Decide)
	return h, store, server
}

func seedReview(t *testing.T, store *review.Memory) review.Item {
	t.Helper()
	ctx := lib.WithTenant(context.Background(), uuid.MustParse(reviewTenant))
	item, err := store.Create(ctx, review.Item{
		RunID: "run-1", Subject: "claim-1",
		Result: json.RawMessage(`{"amount":10}`),
		Schema: json.RawMessage(reviewSchema),
		Role:   "reviewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestReviewHandlerDecides(t *testing.T) {
	_, store, server := reviewHarness(t, "reviewer")
	item := seedReview(t, store)

	body := `{"result":{"amount":42},"reason":"checked"}`
	status, resp := request(t, server, "POST", "/tenants/"+reviewTenant+"/reviews/"+item.ID+"/decisions", body)
	if status != 200 {
		t.Fatalf("status = %d body = %s", status, resp)
	}
	if !strings.Contains(resp, `"DECIDED"`) || !strings.Contains(resp, `"amount":42`) {
		t.Fatalf("unexpected body = %s", resp)
	}
}

func TestReviewHandlerRejectsWrongRole(t *testing.T) {
	_, store, server := reviewHarness(t, "user")
	item := seedReview(t, store)

	body := `{"result":{"amount":42}}`
	status, resp := request(t, server, "POST", "/tenants/"+reviewTenant+"/reviews/"+item.ID+"/decisions", body)
	if status != 403 {
		t.Fatalf("status = %d body = %s", status, resp)
	}
}

func TestReviewHandlerRejectsSchemaMismatch(t *testing.T) {
	_, store, server := reviewHarness(t, "reviewer")
	item := seedReview(t, store)

	body := `{"result":{"amount":"not-a-number"}}`
	status, resp := request(t, server, "POST", "/tenants/"+reviewTenant+"/reviews/"+item.ID+"/decisions", body)
	if status != 400 {
		t.Fatalf("status = %d body = %s", status, resp)
	}
}

func TestReviewHandlerListFilters(t *testing.T) {
	_, store, server := reviewHarness(t, "reviewer")
	seedReview(t, store)

	status, resp := request(t, server, "GET", "/tenants/"+reviewTenant+"/reviews/?status=OPEN", "")
	if status != 200 || !strings.Contains(resp, `"total":1`) {
		t.Fatalf("status = %d body = %s", status, resp)
	}
	status, resp = request(t, server, "GET", "/tenants/"+reviewTenant+"/reviews/?status=DECIDED", "")
	if status != 200 || !strings.Contains(resp, `"total":0`) {
		t.Fatalf("status = %d body = %s", status, resp)
	}
}

func TestReviewHandlerRejectsTenantHeaderMismatch(t *testing.T) {
	_, store, server := reviewHarness(t, "reviewer")
	item := seedReview(t, store)

	r := httptest.NewRequest("GET", "/tenants/"+reviewTenant+"/reviews/"+item.ID, nil)
	r.Header.Set("X-Tenant-ID", "22222222-2222-2222-2222-222222222222")
	res, err := server.Test(r)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		t.Fatalf("status = %d", res.StatusCode)
	}
}
