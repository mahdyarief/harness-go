package repositories

import (
	"context"
	"fmt"

	"hsg/internal/models"

	"github.com/google/uuid"
)

// UserRepository manages the public mirror of Authula's users. Authula owns
// the source rows; this table follows them and adds the application role.
type UserRepository struct {
	BaseRepository
}

// Upsert copies an Authula user into the mirror, inserting or refreshing it.
// The role is left alone: a new row gets the column default.
func (r *UserRepository) Upsert(ctx context.Context, u models.User) error {
	_, err := r.execContext(ctx, `
		INSERT INTO users (id, name, email, image)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		SET name = EXCLUDED.name, email = EXCLUDED.email, image = EXCLUDED.image, updated_at = now()`,
		u.ID, u.Name, u.Email, u.Image,
	)
	return err
}

// Delete removes one mirror row. Only for compensating a failed create; the
// mirror otherwise follows Authula through the hooks, never the reverse.
func (r *UserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.execContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

// Reconcile copies every Authula user into the mirror, repairing rows a
// failed hook left missing or stale. Mirror rows of users deleted in Authula
// are kept: they can no longer sign in, and deleting here would wipe the
// roles if Authula ever pointed at the wrong schema.
// ponytail: runs once per boot; a trigger on authula.users would keep the
// mirror exact if drift between boots ever matters.
func (r *UserRepository) Reconcile(ctx context.Context) error {
	_, err := r.execContext(ctx, `
		INSERT INTO users (id, name, email, image)
		SELECT id, name, email, image FROM authula.users
		ON CONFLICT (id) DO UPDATE
		SET name = EXCLUDED.name, email = EXCLUDED.email, image = EXCLUDED.image, updated_at = now()
		WHERE (users.name, users.email, users.image) IS DISTINCT FROM (EXCLUDED.name, EXCLUDED.email, EXCLUDED.image)`)
	return err
}

// SetRole changes a mirrored user's role.
func (r *UserRepository) SetRole(ctx context.Context, id uuid.UUID, role string) error {
	res, err := r.execContext(ctx, `UPDATE users SET role = $2, updated_at = now() WHERE id = $1`, id, role)
	if err != nil {
		return err
	}
	return requireAffected(res, "user")
}

// Role returns a mirrored user's role.
func (r *UserRepository) Role(ctx context.Context, id uuid.UUID) (string, error) {
	var role string
	err := r.queryRowContext(ctx, `SELECT role FROM users WHERE id = $1`, id).Scan(&role)
	return role, translateNotFound("users", err)
}

const userColumns = `
	id, created_at, updated_at, name, email, image, role`

func scanUser(row rowScanner) (models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.CreatedAt, &u.UpdatedAt, &u.Name, &u.Email, &u.Image, &u.Role)
	return u, translateNotFound("users", err)
}

// GetByID returns one mirrored user, including the application role.
func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (models.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE id = $1`
	return scanUser(r.queryRowContext(ctx, query, id))
}

// userSortColumns whitelists the columns a `sorted` value may order by.
var userSortColumns = map[string]string{
	"name":       "name",
	"email":      "email",
	"created_at": "created_at",
	"updated_at": "updated_at",
}

// userFilters builds the WHERE conditions shared by List and Count.
func userFilters(p ListParams) *whereBuilder {
	w := newWhere(`1=1`)
	w.search(p.Search, "name", "email")
	return w
}

// List returns one page of mirrored users, optionally filtered by a
// case-insensitive name/email search and ordered by p.Sorted. Callers clamp
// the offset/limit first (see dtos.ListQuery.Clamp).
func (r *UserRepository) List(ctx context.Context, p ListParams) ([]models.User, error) {
	where, args, limit, offset := userFilters(p).page(p)

	query := fmt.Sprintf(`
		SELECT %s FROM users
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d`,
		userColumns, where,
		resolveOrder(p.Sorted, userSortColumns, "created_at DESC"), limit, offset)

	rows, err := r.queryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return collectRows(rows, scanUser)
}

// Count returns how many mirrored users match the List filters, for the
// response meta block.
func (r *UserRepository) Count(ctx context.Context, p ListParams) (int64, error) {
	return r.countRows(ctx, "users", userFilters(p))
}
