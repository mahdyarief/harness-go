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

type auditHandler struct {
	app  *app.Application
	repo *repositories.AuditRepository
}

func newAuditHandler(a *app.Application) *auditHandler {
	return &auditHandler{app: a, repo: repositories.NewAuditRepository(a.TenantDB)}
}

// List returns the platform-wide audit trail. The route is guarded by the
// SuperUser middleware and the repository re-proves the role in the transaction.
func (h *auditHandler) List(c fiber.Ctx) error {
	actor, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	var q dtos.AuditQuery
	if err = lib.ValidateRequestQuery(c, &q); err != nil {
		return err
	}
	q.Clamp()

	filters, err := auditFiltersFrom(q)
	if err != nil {
		return err
	}

	entries, err := h.repo.List(c.Context(), actor, filters)
	if err != nil {
		return err
	}
	total, err := h.repo.Count(c.Context(), actor, filters)
	if err != nil {
		return err
	}
	return dtos.List(c, entries, dtos.ListMeta(int(total), q.Offset, q.Limit))
}

// Get returns one audit entry by id.
func (h *auditHandler) Get(c fiber.Ctx) error {
	actor, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	id, err := pathID(c, "audit log")
	if err != nil {
		return err
	}
	entry, err := h.repo.Get(c.Context(), actor, id)
	if err != nil {
		return err
	}
	return dtos.Get(c, entry)
}

func auditFiltersFrom(q dtos.AuditQuery) (repositories.AuditFilters, error) {
	f := repositories.AuditFilters{
		ListParams: repositories.ListParams{
			Offset: q.Offset,
			Limit:  q.Limit,
			Search: q.Search,
			Sorted: q.Sorted,
		},
		Action:     q.Action,
		EntityType: q.EntityType,
		From:       q.From,
		To:         q.To,
	}
	var err error
	if f.EntityID, err = optionalUUID(q.EntityID); err != nil {
		return f, err
	}
	if f.ActorID, err = optionalUUID(q.ActorID); err != nil {
		return f, err
	}
	if f.TenantID, err = optionalUUID(q.TenantID); err != nil {
		return f, err
	}
	return f, nil
}

func optionalUUID(raw string) (*uuid.UUID, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, apperr.New(apperr.KindBadRequest, "ID filter tidak valid")
	}
	return &id, nil
}
