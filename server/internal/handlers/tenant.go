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

type TenantHandler struct {
	repo *repositories.TenantRepository
}

func NewTenantHandler(a *app.Application) *TenantHandler {
	return &TenantHandler{repo: repositories.NewTenantRepository(a.TenantDB)}
}
func (h *TenantHandler) List(c fiber.Ctx) error {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	result, err := h.repo.List(c.Context(), uid)
	if err != nil {
		return err
	}
	return dtos.OK(c, result)
}
func (h *TenantHandler) Create(c fiber.Ctx) error {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	var body dtos.CreateTenantBody
	if err = c.Bind().Body(&body); err != nil {
		return apperr.New(apperr.KindBadRequest, "invalid tenant body")
	}
	result, err := h.repo.Create(c.Context(), uid, body.AdminUserID, body.Name)
	if err != nil {
		return err
	}
	return dtos.Created(c, result, "tenant created")
}
func (h *TenantHandler) Members(c fiber.Ctx) error {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	tenant, err := uuid.Parse(c.Params("tenant_id"))
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "invalid tenant id")
	}
	result, err := h.repo.Members(c.Context(), uid, tenant)
	if err != nil {
		return err
	}
	return dtos.OK(c, result)
}

// PlatformList returns every tenant so the user drawer can offer a picker.
// Restricted to super users at the route; the repository proves the role
// again inside the transaction.
func (h *TenantHandler) PlatformList(c fiber.Ctx) error {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	result, err := h.repo.ListAll(c.Context(), uid)
	if err != nil {
		return err
	}
	return dtos.OK(c, result)
}

// PlatformDirectory returns every tenant with its members, grouped, for the
// super user's tenant page. Restricted to super users at the route; the
// repository proves the role again inside the transaction.
func (h *TenantHandler) PlatformDirectory(c fiber.Ctx) error {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	result, err := h.repo.Directory(c.Context(), uid)
	if err != nil {
		return err
	}
	return dtos.OK(c, result)
}
func (h *TenantHandler) AssignMember(c fiber.Ctx) error {
	uid, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	tenant, err := uuid.Parse(c.Params("tenant_id"))
	if err != nil {
		return apperr.New(apperr.KindBadRequest, "invalid tenant id")
	}
	var body dtos.TenantMemberBody
	if err = c.Bind().Body(&body); err != nil {
		return apperr.New(apperr.KindBadRequest, "invalid membership body")
	}
	if err = h.repo.AssignMember(c.Context(), uid, tenant, body.UserID, body.Role); err != nil {
		return err
	}
	return dtos.Message(c, fiber.StatusOK, "member assigned")
}
