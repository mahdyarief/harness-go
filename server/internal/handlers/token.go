package handlers

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"hsg/internal/app"
	"hsg/internal/dtos"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
	"hsg/internal/repositories"
)

// tokenHandler owns the token allocation API: platform admins grant pools to
// tenants and see every tenant's ledger; tenant members see their own usage.
type tokenHandler struct {
	app  *app.Application
	repo *repositories.TokenRepository
}

func newTokenHandler(a *app.Application) *tokenHandler {
	return &tokenHandler{
		app:  a,
		repo: repositories.NewTokenRepository(a.TenantDB),
	}
}

func (h *tokenHandler) actorID(c fiber.Ctx) (uuid.UUID, error) {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return uuid.Nil, apperr.ErrUnauthorized
	}
	return uid, nil
}

// ListBalances returns every tenant's current balance, newest-updated first.
// Only a platform admin may call this.
func (h *tokenHandler) ListBalances(c fiber.Ctx) error {
	uid, err := h.actorID(c)
	if err != nil {
		return err
	}
	balances, err := h.repo.Balances(c.Context(), uid)
	if err != nil {
		return err
	}
	return dtos.List(c, balances, nil)
}

// GetBalance returns one tenant's current balance. A platform admin may read
// any tenant; a tenant member may read their own tenant.
func (h *tokenHandler) GetBalance(c fiber.Ctx) error {
	uid, err := h.actorID(c)
	if err != nil {
		return err
	}
	tenant, err := uuid.Parse(c.Params("tenant_id"))
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "invalid tenant id")
	}
	b, err := h.repo.Balance(c.Context(), uid, tenant)
	if err != nil {
		return err
	}
	return dtos.Get(c, b)
}

// TopUp grants tokens to a tenant and records the addition in the ledger.
// Only a platform admin may call this.
func (h *tokenHandler) TopUp(c fiber.Ctx) error {
	uid, err := h.actorID(c)
	if err != nil {
		return err
	}
	var body dtos.TopUpBody
	if err = lib.ValidateRequestBody(c, &body); err != nil {
		return err
	}
	tenant, err := uuid.Parse(c.Params("tenant_id"))
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "invalid tenant id")
	}
	b, err := h.repo.TopUp(c.Context(), uid, tenant, body.Amount, body.Note)
	if err != nil {
		return err
	}
	return dtos.Created(c, b, "Token berhasil ditambahkan")
}

// ListTopups returns the top-up history for one tenant, newest first.
// A platform admin may read any tenant; a tenant_admin may read their tenant.
func (h *tokenHandler) ListTopups(c fiber.Ctx) error {
	uid, err := h.actorID(c)
	if err != nil {
		return err
	}
	tenant, err := uuid.Parse(c.Params("tenant_id"))
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "invalid tenant id")
	}
	var q dtos.ListQuery
	if err = lib.ValidateRequestQuery(c, &q); err != nil {
		return err
	}
	q.Clamp()
	topups, err := h.repo.Topups(c.Context(), uid, tenant, q.Limit, q.Offset)
	if err != nil {
		return err
	}
	total, err := h.repo.CountTopups(c.Context(), uid, tenant)
	if err != nil {
		return err
	}
	return dtos.List(c, topups, dtos.ListMeta(total, q.Offset, q.Limit))
}

// ListUsages returns the consumption ledger for one tenant, newest first.
// The repository applies the visibility tier from the caller's role.
func (h *tokenHandler) ListUsages(c fiber.Ctx) error {
	uid, err := h.actorID(c)
	if err != nil {
		return err
	}
	tenant, err := uuid.Parse(c.Params("tenant_id"))
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "invalid tenant id")
	}
	var q dtos.ListQuery
	if err = lib.ValidateRequestQuery(c, &q); err != nil {
		return err
	}
	q.Clamp()

	// Visibility tiering is resolved inside the repository from the caller's
	// role and membership, not from any request parameter.
	usages, err := h.repo.Usages(c.Context(), uid, tenant, q.Limit, q.Offset)
	if err != nil {
		return err
	}
	total, err := h.repo.CountUsages(c.Context(), uid, tenant)
	if err != nil {
		return err
	}
	return dtos.List(c, usages, dtos.ListMeta(total, q.Offset, q.Limit))
}
