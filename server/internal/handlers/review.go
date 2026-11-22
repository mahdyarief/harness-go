package handlers

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"hsg/internal/app"
	"hsg/internal/dtos"
	"hsg/internal/harness/review"
	runlog2 "hsg/internal/harness/runlog"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
	"hsg/internal/lib/validator"
	"hsg/internal/repositories"
)

const reviewRoleRegex = `^(tenant_admin|reviewer)$`

// reviewHandler exposes the tenant-scoped review inbox. The store is
// injected (memory in tests, Postgres in production) so the inbox is opt-in
// and the same handler serves both implementations.
type reviewHandler struct {
	store review.Store
	// role resolves the caller's tenant role. It is a field so tests can
	// substitute a fixed role without a database.
	role func(ctx fiber.Ctx, tenantID string, userID string) (string, error)
	// logDecided appends review/decided to the run log. Nil is allowed (tests,
	// and runs without a log): the decision is still durable in the inbox.
	logDecided func(ctx context.Context, item review.Item, decision review.Decision)
}

func newReviewHandler(a *app.Application) *reviewHandler {
	tenants := repositories.NewTenantRepository(a.TenantDB)
	store := repositories.NewReviewRepository(a.TenantDB)
	logs := repositories.NewRunLogRepository(a.TenantDB)
	return &reviewHandler{
		store: store,
		role: func(c fiber.Ctx, tenantID, userID string) (string, error) {
			tenant, err := uuid.Parse(tenantID)
			if err != nil {
				return "", apperr.New(apperr.KindBadRequest, "tenant_id tidak valid")
			}
			uid, err := uuid.Parse(userID)
			if err != nil {
				return "", apperr.New(apperr.KindBadRequest, "user id tidak valid")
			}
			return tenants.MemberRole(c.Context(), tenant, uid)
		},
		logDecided: func(ctx context.Context, item review.Item, decision review.Decision) {
			if _, err := uuid.Parse(item.RunID); err != nil {
				return
			}
			_ = logs.AppendEvent(ctx, runlog2.ReviewDecidedEvent(item.RunID, decision.Reviewer, runlog2.ReviewDecided{
				ItemID: item.ID, Subject: item.Subject, Reviewer: decision.Reviewer,
				Role: decision.Role, Reason: decision.Reason,
				Result: runlog2.Compact(string(decision.Result)),
			}))
		},
	}
}

func reviewScope(c fiber.Ctx) (tenant, uid string, err error) {
	id, err := lib.ContextGetUID(c)
	if err != nil {
		return "", "", err
	}
	raw := c.Params("tenant_id")
	if raw == "" {
		return "", "", apperr.New(apperr.KindBadRequest, "tenant wajib ada pada path")
	}
	if header := c.Get("X-Tenant-ID"); header != "" && header != raw {
		return "", "", apperr.New(apperr.KindBadRequest, "Tenant pada header dan path tidak cocok")
	}
	return raw, id.String(), nil
}

// List handles GET /tenants/:tenant_id/reviews?status=OPEN&limit=20&offset=0.
func (h *reviewHandler) List(c fiber.Ctx) error {
	tenant, _, err := reviewScope(c)
	if err != nil {
		return err
	}
	limit, offset := 20, 0
	if raw := c.Query("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 100 {
			return apperr.New(apperr.KindBadRequest, "limit tidak valid")
		}
	}
	if raw := c.Query("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return apperr.New(apperr.KindBadRequest, "offset tidak valid")
		}
	}
	tid, err := uuid.Parse(tenant)
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "tenant_id tidak valid")
	}
	ctx := lib.WithTenant(c.Context(), tid)
	items, total, err := h.store.List(ctx, c.Query("status"), limit, offset)
	if err != nil {
		return err
	}
	return dtos.List(c, items, dtos.ListMeta(total, offset, limit))
}

// Get handles GET /tenants/:tenant_id/reviews/:id.
func (h *reviewHandler) Get(c fiber.Ctx) error {
	tenant, _, err := reviewScope(c)
	if err != nil {
		return err
	}
	tid, err := uuid.Parse(tenant)
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "tenant_id tidak valid")
	}
	ctx := lib.WithTenant(c.Context(), tid)
	item, err := h.store.Get(ctx, c.Params("id"))
	if err != nil {
		return mapReviewError(err)
	}
	return dtos.Get(c, item)
}

type reviewDecisionBody struct {
	Reviewer  string                 `json:"reviewer"`
	Role      string                 `json:"role"`
	Result    json.RawMessage        `json:"result"`
	Overrides []review.FieldOverride `json:"overrides"`
	Reason    string                 `json:"reason"`
}

func (reviewDecisionBody) Validate(v *validator.MapValidator) {
	v.Field("reviewer").String().MaxLen(200)
	v.Field("role").Regex(reviewRoleRegex)
	v.Field("reason").String().MaxLen(2000)
}

// Decide handles POST /tenants/:tenant_id/reviews/:id/decisions. The caller's
// role must be at least the role the item requires; the declared role must
// match the caller's actual tenant role to prevent privilege escalation.
func (h *reviewHandler) Decide(c fiber.Ctx) error {
	tenant, uid, err := reviewScope(c)
	if err != nil {
		return err
	}
	var body reviewDecisionBody
	if err = lib.ValidateRequestBody(c, &body); err != nil {
		return err
	}
	actual, err := h.role(c, tenant, uid)
	if err != nil {
		return err
	}
	if body.Role == "" {
		body.Role = actual
	}
	if body.Role != actual {
		return apperr.New(apperr.KindForbidden, "peran tidak sesuai dengan pengguna")
	}
	if body.Reviewer == "" {
		body.Reviewer = uid
	}
	if len(body.Result) == 0 {
		return apperr.New(apperr.KindBadRequest, "result wajib ada")
	}
	decision := review.Decision{
		Reviewer: body.Reviewer, Role: body.Role, Result: body.Result,
		Overrides: body.Overrides, Reason: body.Reason, At: time.Now().UTC(),
	}
	tid, err := uuid.Parse(tenant)
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "tenant_id tidak valid")
	}
	ctx := lib.WithTenant(c.Context(), tid)
	item, err := h.store.Decide(ctx, c.Params("id"), decision)
	if err != nil {
		return mapReviewError(err)
	}
	if h.logDecided != nil {
		h.logDecided(ctx, item, decision)
	}
	return dtos.Get(c, item)
}

func mapReviewError(err error) error {
	switch err {
	case review.ErrNotFound:
		return apperr.New(apperr.KindNotFound, "item review tidak ditemukan")
	case review.ErrConflict:
		return apperr.New(apperr.KindConflict, "item review sudah diputuskan")
	case review.ErrForbidden:
		return apperr.New(apperr.KindForbidden, "peran tidak boleh memutuskan item ini")
	case review.ErrSchema:
		return apperr.New(apperr.KindBadRequest, "hasil keputusan tidak sesuai skema")
	default:
		return err
	}
}
