// Package review is the durable inbox of results awaiting a person's
// decision. A Policy (a reducer over the team result) proposes items; a
// reviewer decides through the REST handler; the decision history is kept
// and the latest decision is the one that counts. Every decision is also
// written to the run log as review/decided.
//
// Port of DSH interaction/user-questions and user-approval.
package review

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"hsg/internal/harness/evidence"
	"hsg/internal/harness/schema"
	"hsg/internal/harness/services"
)

// evidenceRef is the citation shape stored on an item; it mirrors the
// evidence.Citation the submission carried.
type evidenceRef = evidence.Citation

// Status of an inbox item.
const (
	StatusOpen    = "OPEN"
	StatusDecided = "DECIDED"
	StatusExpired = "EXPIRED"
)

// Roles that may decide. tenant_admin outranks reviewer; a role not in this
// list is rejected with 403.
var RoleRank = map[string]int{"reviewer": 1, "tenant_admin": 2}

// Item is one proposed result in the inbox. Subject is an opaque id from the
// consumer (a claim id, an invoice id); Result is the structured JSON the
// run proposed; Evidence is the grounded provenance behind it.
type Item struct {
	ID        string          `json:"id"`
	RunID     string          `json:"run_id"`
	Subject   string          `json:"subject"`
	Result    json.RawMessage `json:"result"`
	Schema    json.RawMessage `json:"schema,omitempty"`
	Evidence  []evidenceRef   `json:"evidence,omitempty"`
	Reason    string          `json:"reason"`
	Role      string          `json:"role"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	Decisions []Decision      `json:"decisions,omitempty"`
}

// Decision is one reviewer verdict. Result is the final structured JSON
// (validated against the item's schema); Overrides lists the per-field
// changes the reviewer made. History is append-only.
type Decision struct {
	Reviewer  string          `json:"reviewer"`
	Role      string          `json:"role"`
	Result    json.RawMessage `json:"result"`
	Overrides []FieldOverride `json:"overrides,omitempty"`
	Reason    string          `json:"reason"`
	At        time.Time       `json:"at"`
}

// FieldOverride records one changed field: path inside the result, old and
// new value.
type FieldOverride struct {
	Path string          `json:"path"`
	From json.RawMessage `json:"from"`
	To   json.RawMessage `json:"to"`
}

// Store is the durable inbox. Create is idempotent per (run, subject): a
// re-run of the same subject replaces the open item instead of duplicating
// it. Decide appends to the history and closes the item.
type Store interface {
	Create(ctx context.Context, item Item) (Item, error)
	Get(ctx context.Context, id string) (Item, error)
	List(ctx context.Context, status string, limit, offset int) ([]Item, int, error)
	Decide(ctx context.Context, id string, decision Decision) (Item, error)
	// Await blocks until the item is decided or the context ends. It is the
	// wait side of ApprovalMode "wait".
	Await(ctx context.Context, id string) (Decision, error)
}

// Policy decides which results go to the inbox. It is a reducer over the
// team result (Task 4): return true to enqueue. SampleNoReview is the
// fraction (0..1) of results the policy passed that are enqueued anyway, so
// false negatives can be measured against a human sample.
type Policy func(result services.Outcome) bool

// Sampled wraps a policy with the measurement sample rate.
func Sampled(policy Policy, sampleNoReview float64, rand float64) Policy {
	return func(result services.Outcome) bool {
		if policy(result) {
			return true
		}
		return rand < sampleNoReview
	}
}

// ErrNotFound is returned when the item id does not exist.
var ErrNotFound = errors.New("review: item not found")

// ErrConflict is returned when the item is already decided.
var ErrConflict = errors.New("review: item already decided")

// ErrForbidden is returned when the reviewer's role is below the required
// role.
var ErrForbidden = errors.New("review: role may not decide this item")

// ErrSchema is returned when the decision result fails the item's schema.
var ErrSchema = errors.New("review: decision result fails the item schema")

// Memory is the in-memory Store: tests and experiments without a database.
type Memory struct {
	mu     sync.Mutex
	items  map[string]*Item
	order  []string
	waiter map[string][]chan Decision
	next   int
}

func NewMemory() *Memory {
	return &Memory{items: map[string]*Item{}, waiter: map[string][]chan Decision{}}
}

func (m *Memory) Create(_ context.Context, item Item) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.order {
		existing := m.items[id]
		if existing.RunID == item.RunID && existing.Subject == item.Subject && existing.Status == StatusOpen {
			// A re-run replaces the open item for the same subject.
			item.ID = existing.ID
			item.CreatedAt = existing.CreatedAt
			*m.items[item.ID] = item
			return item, nil
		}
	}
	m.next++
	if item.ID == "" {
		item.ID = "rev-" + itoa(m.next)
	}
	if item.Status == "" {
		item.Status = StatusOpen
	}
	item.CreatedAt = time.Now().UTC()
	stored := item
	m.items[item.ID] = &stored
	m.order = append(m.order, item.ID)
	return item, nil
}

func (m *Memory) Get(_ context.Context, id string) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item, ok := m.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	return *item, nil
}

func (m *Memory) List(_ context.Context, status string, limit, offset int) ([]Item, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Item, 0, len(m.order))
	for _, id := range m.order {
		item := m.items[id]
		if status != "" && item.Status != status {
			continue
		}
		out = append(out, *item)
	}
	total := len(out)
	if offset > len(out) {
		offset = len(out)
	}
	if offset+limit > len(out) {
		limit = len(out) - offset
	}
	if limit < 0 {
		limit = 0
	}
	return out[offset : offset+limit], total, nil
}

func (m *Memory) Decide(_ context.Context, id string, decision Decision) (Item, error) {
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return Item{}, ErrNotFound
	}
	if item.Status != StatusOpen {
		m.mu.Unlock()
		return Item{}, ErrConflict
	}
	if RoleRank[decision.Role] < RoleRank[item.Role] {
		m.mu.Unlock()
		return Item{}, ErrForbidden
	}
	if item.Schema != nil {
		if err := validateResult(item.Schema, decision.Result); err != nil {
			m.mu.Unlock()
			return Item{}, err
		}
	}
	decision.At = time.Now().UTC()
	item.Decisions = append(item.Decisions, decision)
	item.Status = StatusDecided
	result := *item
	waiters := m.waiter[id]
	delete(m.waiter, id)
	m.mu.Unlock()
	for _, ch := range waiters {
		ch <- decision
		close(ch)
	}
	return result, nil
}

func (m *Memory) Await(ctx context.Context, id string) (Decision, error) {
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return Decision{}, ErrNotFound
	}
	if item.Status == StatusDecided && len(item.Decisions) > 0 {
		decision := item.Decisions[len(item.Decisions)-1]
		m.mu.Unlock()
		return decision, nil
	}
	ch := make(chan Decision, 1)
	m.waiter[id] = append(m.waiter[id], ch)
	m.mu.Unlock()
	select {
	case decision := <-ch:
		return decision, nil
	case <-ctx.Done():
		return Decision{}, ctx.Err()
	}
}

// Expire marks stale open items EXPIRED; the caller decides what stale means.
func (m *Memory) Expire(olderThan time.Duration) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	expired := 0
	for _, id := range m.order {
		item := m.items[id]
		if item.Status == StatusOpen && time.Since(item.CreatedAt) >= olderThan {
			item.Status = StatusExpired
			expired++
		}
	}
	return expired
}

// validateResult checks the decision result against the item's schema; a
// decision that fails the schema is rejected with ErrSchema.
func validateResult(schemaJSON, result json.RawMessage) error {
	compiled, err := schema.Compile(schemaJSON)
	if err != nil {
		return err
	}
	if violations := compiled.Validate(result); len(violations) > 0 {
		return ErrSchema
	}
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// ApprovalAnswer turns a review Store into an interactive approval answerer.
// An "ask" creates an inbox item for the tool call and blocks on Await until
// a reviewer decides. Approval means a decision whose result is not false
// and whose reason is not a rejection. The caller supplies the run id, role
// the item requires, and a timeout through the context.
func ApprovalAnswer(store Store, runID, role string) services.ApprovalAnswer {
	return func(ctx context.Context, call services.ToolInvocation) (bool, error) {
		result, _ := json.Marshal(map[string]any{"tool": call.Name, "arguments": call.Arguments})
		item, err := store.Create(ctx, Item{
			RunID:   runID,
			Subject: "approval:" + call.Name,
			Result:  result,
			Reason:  "persetujuan untuk alat " + call.Name,
			Role:    role,
		})
		if err != nil {
			return false, err
		}
		decision, err := store.Await(ctx, item.ID)
		if err != nil {
			return false, err
		}
		var verdict struct {
			Approved *bool `json:"approved"`
		}
		_ = json.Unmarshal(decision.Result, &verdict)
		if verdict.Approved != nil {
			return *verdict.Approved, nil
		}
		return len(decision.Result) > 0 && string(decision.Result) != "null", nil
	}
}
