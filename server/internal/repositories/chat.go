package repositories

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"hsg/internal/lib/apperr"
	"hsg/internal/models"
)

// ChatRepository manages ai_chat_sessions and its dependent tables. Every
// method takes the caller's tenant because the tables are tenant-scoped by RLS:
// a query without app.tenant_id set returns nothing rather than everything.
type ChatRepository struct{ db *sql.DB }

func NewChatRepository(db *sql.DB) *ChatRepository { return &ChatRepository{db: db} }

// begin starts a transaction with the caller's user identity, then proves the
// tenant membership and pins the tenant for the rest of the transaction.
func (r *ChatRepository) begin(ctx context.Context, user uuid.UUID, scope models.ChatScope) (*sql.Tx, error) {
	if r == nil || r.db == nil || user == uuid.Nil || (scope.Kind != models.ChatPlatformPersonal && scope.Kind != models.ChatTenantPersonal) || (!scope.Platform() && scope.TenantID == uuid.Nil) {
		return nil, apperr.ErrForbidden
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	unsafe, err := TenantRoleUnsafe(ctx, tx)
	if err != nil || unsafe {
		tx.Rollback()
		if err != nil {
			return nil, err
		}
		return nil, apperr.ErrForbidden
	}
	// The membership row itself is RLS-guarded, so the identity must be set
	// before the lookup or the policy hides the caller's own membership.
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.user_id',$1,true), set_config('app.tenant_id','',true)`, user.String()); err != nil {
		tx.Rollback()
		return nil, err
	}
	var platform bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='super_user')`, user).Scan(&platform); err != nil {
		tx.Rollback()
		return nil, err
	}
	if scope.Platform() {
		if !platform {
			tx.Rollback()
			return nil, apperr.ErrForbidden
		}
		return tx, nil
	}
	if platform {
		tx.Rollback()
		return nil, apperr.ErrForbidden
	}
	tenant := scope.TenantID
	var role string
	err = tx.QueryRowContext(ctx, `SELECT role FROM tenant_memberships WHERE user_id=$1 AND tenant_id IS NOT DISTINCT FROM $2::uuid`, user, tenant).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		return nil, apperr.ErrForbidden
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, tenant.String()); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

const sessionColumns = `id,tenant_id,user_id,title,created_at,updated_at`

func scanSession(row rowScanner) (models.ChatSession, error) {
	var s models.ChatSession
	err := row.Scan(&s.ID, &s.TenantID, &s.UserID, &s.Title, &s.CreatedAt, &s.UpdatedAt)
	s.Scope = models.ChatTenantPersonal
	if s.TenantID == nil {
		s.Scope = models.ChatPlatformPersonal
	}
	return s, translateNotFound("ai_chat_sessions", err)
}

// ListSessions returns one page of the caller's sessions, newest first.
func (r *ChatRepository) ListSessions(ctx context.Context, user uuid.UUID, scope models.ChatScope, p ListParams) ([]models.ChatSession, error) {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT `+sessionColumns+`
		FROM ai_chat_sessions
		WHERE tenant_id IS NOT DISTINCT FROM $1::uuid AND user_id=$2
		ORDER BY updated_at DESC
		LIMIT $3 OFFSET $4`,
		scope.DatabaseTenantID(), user, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	sessions, err := collectRows(rows, scanSession)
	if err != nil {
		return nil, err
	}
	return sessions, tx.Commit()
}

// CountSessions returns the caller's session total for pagination metadata.
func (r *ChatRepository) CountSessions(ctx context.Context, user uuid.UUID, scope models.ChatScope) (int, error) {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM ai_chat_sessions WHERE tenant_id IS NOT DISTINCT FROM $1::uuid AND user_id=$2`, scope.DatabaseTenantID(), user).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, tx.Commit()
}

// GetSession returns one session owned by the caller.
func (r *ChatRepository) GetSession(ctx context.Context, user uuid.UUID, scope models.ChatScope, session uuid.UUID) (models.ChatSession, error) {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return models.ChatSession{}, err
	}
	defer tx.Rollback()
	s, err := scanSession(tx.QueryRowContext(ctx, `
		SELECT `+sessionColumns+` FROM ai_chat_sessions
		WHERE id=$1 AND tenant_id IS NOT DISTINCT FROM $2::uuid AND user_id=$3`, session, scope.DatabaseTenantID(), user))
	if err != nil {
		return s, err
	}
	return s, tx.Commit()
}

// ErrNoTenantMembership marks an account that is not assigned to any tenant.
// It stays a 403 for the chat endpoints, and lets the access probe tell this
// case apart from other forbidden outcomes.
var ErrNoTenantMembership = apperr.New(apperr.KindForbidden, "Akun Anda belum memiliki keanggotaan tenant. Hubungi admin untuk menambahkan akun Anda ke tenant.")

// ResolveScope returns the tenant the caller chats in. A non-nil preferred
// tenant (the switcher's choice) wins, but only when the caller holds a
// membership there; otherwise the oldest membership is the deterministic
// default. The membership row is always the proof that the user may use it.
func (r *ChatRepository) ResolveScope(ctx context.Context, user, preferred uuid.UUID) (models.ChatScope, error) {
	if r == nil || r.db == nil || user == uuid.Nil {
		return models.ChatScope{}, apperr.ErrForbidden
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.ChatScope{}, err
	}
	defer tx.Rollback()
	// tenant_memberships is RLS-guarded on app.user_id, so the identity must be
	// set before the membership lookup can see the caller's own rows.
	unsafe, err := TenantRoleUnsafe(ctx, tx)
	if err != nil || unsafe {
		if err != nil {
			return models.ChatScope{}, err
		}
		return models.ChatScope{}, apperr.ErrForbidden
	}
	if _, err = tx.ExecContext(ctx, `SELECT set_config('app.user_id',$1,true), set_config('app.tenant_id','',true)`, user.String()); err != nil {
		return models.ChatScope{}, err
	}
	var platform bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role='super_user')`, user).Scan(&platform); err != nil {
		return models.ChatScope{}, err
	}
	if platform {
		return models.ChatScope{Kind: models.ChatPlatformPersonal}, tx.Commit()
	}
	var tenant uuid.UUID
	if preferred != uuid.Nil {
		err = tx.QueryRowContext(ctx,
			`SELECT tenant_id FROM tenant_memberships WHERE user_id=$1 AND tenant_id=$2`, user, preferred).Scan(&tenant)
		if errors.Is(err, sql.ErrNoRows) {
			return models.ChatScope{}, apperr.ErrForbidden
		}
	} else {
		err = tx.QueryRowContext(ctx,
			`SELECT tenant_id FROM tenant_memberships WHERE user_id=$1 ORDER BY created_at LIMIT 1`, user).Scan(&tenant)
		if errors.Is(err, sql.ErrNoRows) {
			return models.ChatScope{}, ErrNoTenantMembership
		}
	}
	if err != nil {
		return models.ChatScope{}, err
	}
	return models.ChatScope{Kind: models.ChatTenantPersonal, TenantID: tenant}, tx.Commit()
}

// ExistsSession checks ownership without loading the row.
func (r *ChatRepository) ExistsSession(ctx context.Context, user uuid.UUID, scope models.ChatScope, session uuid.UUID) (bool, error) {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ai_chat_sessions WHERE id=$1 AND tenant_id IS NOT DISTINCT FROM $2::uuid AND user_id=$3)`, session, scope.DatabaseTenantID(), user).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, tx.Commit()
}

// CreateSession inserts a session and returns it with DB timestamps.
func (r *ChatRepository) CreateSession(ctx context.Context, user uuid.UUID, scope models.ChatScope, title string) (models.ChatSession, error) {
	var s models.ChatSession
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return s, err
	}
	defer tx.Rollback()
	id, err := uuid.NewV7()
	if err != nil {
		return s, err
	}
	s.ID, s.TenantID, s.UserID, s.Title, s.Scope = id, scope.DatabaseTenantID(), user, title, scope.Kind
	err = tx.QueryRowContext(ctx, `
		INSERT INTO ai_chat_sessions (id,tenant_id,user_id,title)
		VALUES ($1,$2,$3,$4)
		RETURNING created_at,updated_at`,
		s.ID, s.TenantID, s.UserID, s.Title).Scan(&s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return s, err
	}
	return s, tx.Commit()
}

// UpdateSessionTitle renames a session and bumps its updated_at.
func (r *ChatRepository) UpdateSessionTitle(ctx context.Context, user uuid.UUID, scope models.ChatScope, session uuid.UUID, title string) error {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		UPDATE ai_chat_sessions SET title=$1, updated_at=now()
		WHERE id=$2 AND tenant_id IS NOT DISTINCT FROM $3::uuid AND user_id=$4`, title, session, scope.DatabaseTenantID(), user)
	if err != nil {
		return err
	}
	if err = requireAffected(res, "ai_chat_sessions"); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteSession removes a session; messages, token usage and tool logs cascade.
func (r *ChatRepository) DeleteSession(ctx context.Context, user uuid.UUID, scope models.ChatScope, session uuid.UUID) error {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `DELETE FROM ai_chat_sessions WHERE id=$1 AND tenant_id IS NOT DISTINCT FROM $2::uuid AND user_id=$3`, session, scope.DatabaseTenantID(), user)
	if err != nil {
		return err
	}
	if err = requireAffected(res, "ai_chat_sessions"); err != nil {
		return err
	}
	return tx.Commit()
}

// ListMessages returns the whole transcript of one session, oldest first.
func (r *ChatRepository) ListMessages(ctx context.Context, user uuid.UUID, scope models.ChatScope, session uuid.UUID) ([]models.ChatMessage, error) {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT id,session_id,role,content,tool_call_id,tool_name,finish_reason,"references",created_at
		FROM ai_chat_messages
		WHERE session_id=$1
		ORDER BY created_at ASC`, session)
	if err != nil {
		return nil, err
	}
	messages, err := collectRows(rows, scanMessage)
	if err != nil {
		return nil, err
	}
	return messages, tx.Commit()
}

// ListMessagesPage returns one page of a session's transcript plus the total
// message count. The page is selected newest first, so offset 0 is the tail of
// the conversation, but it is returned oldest first so a caller can prepend it
// above the messages it already holds. ListMessages keeps returning the whole
// transcript because the agent turn path needs the full history.
func (r *ChatRepository) ListMessagesPage(ctx context.Context, user uuid.UUID, scope models.ChatScope, session uuid.UUID, limit, offset int) ([]models.ChatMessage, int, error) {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	// The message policy already hides foreign sessions, but an empty page must
	// not be confused with a session that does not exist.
	var exists bool
	if err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM ai_chat_sessions
		               WHERE id=$1 AND tenant_id IS NOT DISTINCT FROM $2::uuid AND user_id=$3)`,
		session, scope.DatabaseTenantID(), user).Scan(&exists); err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, apperr.ErrNotFound
	}
	var total int
	if err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ai_chat_messages WHERE session_id=$1`, session).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id,session_id,role,content,tool_call_id,tool_name,finish_reason,"references",created_at
		FROM ai_chat_messages
		WHERE session_id=$1
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3`, session, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	messages, err := collectRows(rows, scanMessage)
	if err != nil {
		return nil, 0, err
	}
	// Newest-first selection, oldest-first page.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, total, tx.Commit()
}

func scanMessage(row rowScanner) (models.ChatMessage, error) {
	var m models.ChatMessage
	err := row.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.ToolCallID, &m.ToolName, &m.FinishReason, &m.References, &m.CreatedAt)
	return m, translateNotFound("ai_chat_messages", err)
}

// AppendMessage records one turn and bumps the session's updated_at. Returns a
// rollback function so the caller can commit a whole exchange atomically.
func (r *ChatRepository) AppendMessage(ctx context.Context, user uuid.UUID, scope models.ChatScope, m models.ChatMessage) (models.ChatMessage, error) {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	id, err := uuid.NewV7()
	if err != nil {
		return m, err
	}
	m.ID = id
	// The NOT NULL column has no SQL default on explicit NULL, so an empty
	// references slice must be written rather than letting nil reach the row.
	refs := m.References
	if len(refs) == 0 {
		refs = json.RawMessage("[]")
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO ai_chat_messages (id,session_id,role,content,tool_call_id,tool_name,finish_reason,"references")
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING created_at`,
		m.ID, m.SessionID, m.Role, m.Content, m.ToolCallID, m.ToolName, m.FinishReason, refs).Scan(&m.CreatedAt)
	if err != nil {
		return m, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE ai_chat_sessions SET updated_at=now() WHERE id=$1`, m.SessionID); err != nil {
		return m, err
	}
	return m, tx.Commit()
}

// RecordTokenUsage stores what the provider reported for one assistant turn.
func (r *ChatRepository) RecordTokenUsage(ctx context.Context, user uuid.UUID, scope models.ChatScope, u models.ChatTokenUsage) error {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ai_chat_token_usage (id,session_id,message_id,prompt_tokens,completion_tokens,total_tokens)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		id, u.SessionID, u.MessageID, u.PromptTokens, u.CompletionTokens, u.TotalTokens)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ListToolLogs returns one page of a session's tool invocations, oldest first,
// plus the total count, so long histories can be paged instead of loaded whole.
// The session is proven to belong to the caller first: tool logs inherit the
// session's isolation, so a foreign session yields an empty list rather than
// another user's activity.
func (r *ChatRepository) ListToolLogs(ctx context.Context, user uuid.UUID, scope models.ChatScope, session uuid.UUID, limit, offset int) ([]models.ChatToolLog, int, error) {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	// The tool_select policy already hides other users' logs, but an empty
	// page must not be confused with a session that does not exist.
	var exists bool
	if err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM ai_chat_sessions
		               WHERE id=$1 AND tenant_id IS NOT DISTINCT FROM $2::uuid AND user_id=$3)`,
		session, scope.DatabaseTenantID(), user).Scan(&exists); err != nil {
		return nil, 0, err
	}
	if !exists {
		return nil, 0, apperr.ErrNotFound
	}
	var total int
	if err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ai_chat_tool_logs WHERE session_id=$1`, session).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id,session_id,turn,tool_name,arguments,result,duration_ms,error,created_at
		FROM ai_chat_tool_logs WHERE session_id=$1
		ORDER BY created_at, id
		LIMIT $2 OFFSET $3`, session, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]models.ChatToolLog, 0)
	for rows.Next() {
		var l models.ChatToolLog
		if err = rows.Scan(&l.ID, &l.SessionID, &l.Turn, &l.ToolName, &l.Arguments,
			&l.Result, &l.DurationMs, &l.Error, &l.CreatedAt); err != nil {
			return nil, 0, err
		}
		result = append(result, l)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	return result, total, tx.Commit()
}

// RecordToolLog stores one tool invocation for audit.
func (r *ChatRepository) RecordToolLog(ctx context.Context, user uuid.UUID, scope models.ChatScope, l models.ChatToolLog) error {
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO ai_chat_tool_logs (id,session_id,turn,tool_name,arguments,result,duration_ms,error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		id, l.SessionID, l.Turn, l.ToolName, l.Arguments, l.Result, l.DurationMs, l.Error)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RecordPlatformUsage appends metering independently of assistant persistence.
// Session identifiers are retained as audit metadata, not cascading foreign keys.
func (r *ChatRepository) RecordPlatformUsage(ctx context.Context, user uuid.UUID, scope models.ChatScope, session uuid.UUID, prompt, completion int, failed bool) error {
	if !scope.Platform() {
		return apperr.ErrForbidden
	}
	tx, err := r.begin(ctx, user, scope)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO platform_chat_token_usages (user_id,session_id,prompt_tokens,completion_tokens,total_tokens,failed) VALUES ($1,$2,$3,$4,$5,$6)`, user, session, prompt, completion, prompt+completion, failed)
	if err != nil {
		return err
	}
	return tx.Commit()
}
