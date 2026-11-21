package handlers

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	authulamodels "github.com/Authula/authula/models"

	"hsg/internal/app"
	"hsg/internal/dtos"
	"hsg/internal/lib"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
	"hsg/internal/repositories"
)

type userHandler struct {
	app *app.Application
}

func (h *userHandler) repo() *repositories.UserRepository {
	return &h.app.Repositories.User
}

// tenants reaches the RLS-scoped tenant pool; built per call like the
// tenant handler does.
func (h *userHandler) tenants() *repositories.TenantRepository {
	return repositories.NewTenantRepository(h.app.TenantDB)
}

// List returns the mirrored users, for the platform admin's pickers and
// user management. Restricted to super users at the route.
func (h *userHandler) List(c fiber.Ctx) error {
	var q dtos.ListQuery
	if err := lib.ValidateRequestQuery(c, &q); err != nil {
		return err
	}
	q.Clamp()

	params := listParams(q)

	users, err := h.repo().List(c.Context(), params)
	if err != nil {
		return err
	}
	total, err := h.repo().Count(c.Context(), params)
	if err != nil {
		return err
	}

	return dtos.List(c, users, dtos.ListMeta(int(total), q.Offset, q.Limit))
}

// pairing validates the optional tenant fields together. The validator runs
// on the raw body, so this cross-field rule cannot live in Validate.
func pairing(rawID, role string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(rawID)
	if (trimmed == "") != (role == "") {
		return uuid.Nil, apperr.New(apperr.KindBadRequest, "tenant_id dan tenant_role harus dikirim bersamaan")
	}
	if trimmed == "" {
		return uuid.Nil, nil
	}
	id, err := uuid.Parse(trimmed)
	if err != nil {
		return uuid.Nil, apperr.New(apperr.KindBadRequest, "tenant_id tidak valid")
	}
	return id, nil
}

// Membership returns the tenant a user belongs to, so the edit drawer can
// prefill the tenant picker. A user with no membership answers null.
func (h *userHandler) Membership(c fiber.Ctx) error {
	actor, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	id, err := pathID(c, "user")
	if err != nil {
		return err
	}
	membership, err := h.tenants().Membership(c.Context(), actor, id)
	if err != nil {
		return err
	}
	return dtos.OK(c, membership)
}

// Create provisions a user end to end: the Authula user with a verified
// email, its email-password account (admin-set password), the mirror row and
// the platform role, then optionally the first tenant membership. Restricted
// to super users at the route.
func (h *userHandler) Create(c fiber.Ctx) error {
	actor, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	var body dtos.CreateUserBody
	if err = lib.ValidateRequestBody(c, &body); err != nil {
		return err
	}
	tenant, err := pairing(body.TenantID, body.TenantRole)
	if err != nil {
		return err
	}

	core := h.app.Auth.CoreServices()

	existing, err := core.UserService.GetByEmail(c.Context(), body.Email)
	if err != nil {
		return err
	}
	if existing != nil {
		return apperr.New(apperr.KindConflict, "email sudah terdaftar")
	}

	authUser, err := core.UserService.Create(c.Context(), body.Name, body.Email, true, nil, nil)
	if err != nil {
		return err
	}
	userID, err := uuid.Parse(authUser.ID)
	if err != nil {
		return err
	}

	hash, err := core.PasswordService.Hash(body.Password)
	if err != nil {
		return err
	}
	if _, err = core.AccountService.Create(c.Context(), authUser.ID, authUser.Email, authulamodels.AuthProviderEmail.String(), &hash); err != nil {
		// Leave no half-created account behind: drop the Authula user and
		// its mirror so a retry starts clean.
		_ = core.UserService.Delete(c.Context(), authUser.ID)
		_ = h.repo().Delete(c.Context(), userID)
		return err
	}

	// The AfterCreate hook mirrors the user, but hook failures are logged
	// and swallowed; upsert guarantees the row SetRole needs.
	if err = h.repo().Upsert(c.Context(), models.User{Base: models.Base{ID: userID}, Name: body.Name, Email: body.Email}); err != nil {
		return err
	}
	if err = h.repo().SetRole(c.Context(), userID, body.Role); err != nil {
		return err
	}

	if tenant != uuid.Nil {
		if err = h.tenants().AssignPlatformMember(c.Context(), actor, tenant, userID, body.TenantRole); err != nil {
			return err
		}
	}

	created, err := h.repo().GetByID(c.Context(), userID)
	if err != nil {
		return err
	}
	if err = h.audit(c, actor, repositories.ActionUserCreated, userID, map[string]any{
		"role":        body.Role,
		"tenant_id":   tenant.String(),
		"tenant_role": body.TenantRole,
	}); err != nil {
		return err
	}
	return dtos.Created(c, created, "user created")
}

// audit appends one platform audit entry. It runs after the Authula write
// because Authula owns its own store; a failure surfaces to the caller rather
// than leaving a mutation with no trace.
func (h *userHandler) audit(c fiber.Ctx, actor uuid.UUID, action string, subject uuid.UUID, metadata map[string]any) error {
	entry := repositories.NewAuditRepository(h.app.TenantDB)
	return entry.RecordAction(c.Context(), actor, "super_user", action, "user", &subject, nil, metadata)
}

// Update edits name, email, platform role, password and the tenant
// membership. An absent password leaves credentials alone; an absent tenant
// (dropped by the validator as an empty string) leaves memberships alone.
func (h *userHandler) Update(c fiber.Ctx) error {
	actor, err := lib.ContextGetUID(c)
	if err != nil {
		return apperr.ErrUnauthorized
	}
	id, err := pathID(c, "user")
	if err != nil {
		return err
	}
	var body dtos.UpdateUserBody
	if err = lib.ValidateRequestBody(c, &body); err != nil {
		return err
	}
	tenant, err := pairing(body.TenantID, body.TenantRole)
	if err != nil {
		return err
	}

	core := h.app.Auth.CoreServices()

	authUser, err := core.UserService.GetByID(c.Context(), id.String())
	if err != nil {
		return err
	}
	if authUser == nil {
		return apperr.ErrNotFound
	}

	if body.Email != authUser.Email {
		taken, err := core.UserService.GetByEmail(c.Context(), body.Email)
		if err != nil {
			return err
		}
		if taken != nil {
			return apperr.New(apperr.KindConflict, "email sudah terdaftar")
		}
	}

	if body.Name != authUser.Name || body.Email != authUser.Email {
		authUser.Name = body.Name
		authUser.Email = body.Email
		// Update (not UpdateFields) so the AfterUpdate hook re-mirrors.
		if _, err = core.UserService.Update(c.Context(), authUser); err != nil {
			return err
		}
	}

	if body.Password != "" {
		hash, err := core.PasswordService.Hash(body.Password)
		if err != nil {
			return err
		}
		account, err := core.AccountService.GetByUserIDAndProvider(c.Context(), authUser.ID, authulamodels.AuthProviderEmail.String())
		if err != nil {
			return err
		}
		if account == nil {
			if _, err = core.AccountService.Create(c.Context(), authUser.ID, authUser.Email, authulamodels.AuthProviderEmail.String(), &hash); err != nil {
				return err
			}
		} else {
			account.Password = &hash
			if _, err = core.AccountService.Update(c.Context(), account); err != nil {
				return err
			}
		}
		// A password the admin set ends every session the user holds.
		if err = core.SessionService.DeleteAllByUserID(c.Context(), authUser.ID); err != nil {
			return err
		}
	}

	if _, err = h.repo().GetByID(c.Context(), id); err != nil {
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Kind != apperr.KindNotFound {
			return err
		}
		// The mirror hook failed at create time; repair it here.
		if err = h.repo().Upsert(c.Context(), models.User{Base: models.Base{ID: id}, Name: body.Name, Email: body.Email}); err != nil {
			return err
		}
	}
	if err = h.repo().SetRole(c.Context(), id, body.Role); err != nil {
		return err
	}

	if tenant != uuid.Nil {
		if err = h.tenants().AssignPlatformMember(c.Context(), actor, tenant, id, body.TenantRole); err != nil {
			return err
		}
	}

	updated, err := h.repo().GetByID(c.Context(), id)
	if err != nil {
		return err
	}
	metadata := map[string]any{"role": body.Role, "tenant_id": tenant.String(), "tenant_role": body.TenantRole}
	if body.Password != "" {
		// The password itself is never recorded, only the fact that it changed.
		metadata["password_changed"] = true
	}
	if err = h.audit(c, actor, repositories.ActionUserUpdated, id, metadata); err != nil {
		return err
	}
	return dtos.Response(c, fiber.StatusOK, updated, nil, "user updated")
}
