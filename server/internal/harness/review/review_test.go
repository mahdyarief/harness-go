package review_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"hsg/internal/harness/review"
	"hsg/internal/harness/services"
)

const resultSchema = `{"type":"object","properties":{"verdict":{"type":"string","enum":["accept","reject"]},"note":{"type":"string"}},"required":["verdict"],"additionalProperties":false}`

func openItem(t *testing.T, store *review.Memory) review.Item {
	t.Helper()
	item, err := store.Create(context.Background(), review.Item{
		RunID:   "run-1",
		Subject: "claim-1",
		Result:  json.RawMessage(`{"verdict":"accept"}`),
		Schema:  json.RawMessage(resultSchema),
		Reason:  "low confidence",
		Role:    "reviewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestDecideRequiresRoleAndValidResult(t *testing.T) {
	store := review.NewMemory()
	item := openItem(t, store)

	// A role not in RoleRank cannot decide: zero rank < reviewer's one.
	_, err := store.Decide(context.Background(), item.ID, review.Decision{
		Reviewer: "u", Role: "guest", Result: json.RawMessage(`{"verdict":"accept"}`),
	})
	if !errors.Is(err, review.ErrForbidden) {
		t.Fatalf("err = %v; want ErrForbidden", err)
	}

	// A decision that fails the schema is rejected.
	_, err = store.Decide(context.Background(), item.ID, review.Decision{
		Reviewer: "u", Role: "tenant_admin", Result: json.RawMessage(`{"verdict":"maybe"}`),
	})
	if !errors.Is(err, review.ErrSchema) {
		t.Fatalf("err = %v; want ErrSchema", err)
	}

	// A valid decision closes the item; history contains the failed attempts? No
	// — rejected decisions never entered history.
	decided, err := store.Decide(context.Background(), item.ID, review.Decision{
		Reviewer: "u", Role: "tenant_admin", Result: json.RawMessage(`{"verdict":"reject","note":"need proof"}`),
		Reason: "evidence weak",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != review.StatusDecided || len(decided.Decisions) != 1 {
		t.Fatalf("decided = %+v", decided)
	}
	if decided.Decisions[0].At.IsZero() {
		t.Fatal("decision time missing")
	}
}

func TestDecisionHistoryIsKept(t *testing.T) {
	// The store keeps every accepted decision in history; a second decision
	// on the same item is a conflict, so history grows across reopens: the
	// memory store models the same item re-created for a new review round.
	store := review.NewMemory()
	item, err := store.Create(context.Background(), review.Item{
		RunID: "run-1", Subject: "claim-1", Result: json.RawMessage(`{"verdict":"accept"}`),
		Schema: json.RawMessage(resultSchema), Role: "reviewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Decide(context.Background(), item.ID, review.Decision{
		Reviewer: "a", Role: "tenant_admin", Result: json.RawMessage(`{"verdict":"accept"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Decide(context.Background(), item.ID, review.Decision{
		Reviewer: "b", Role: "tenant_admin", Result: json.RawMessage(`{"verdict":"reject"}`),
	}); !errors.Is(err, review.ErrConflict) {
		t.Fatalf("second decision err = %v; want ErrConflict", err)
	}
	got, err := store.Get(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Decisions) != 1 || got.Decisions[0].Reviewer != "a" {
		t.Fatalf("history = %+v", got.Decisions)
	}
	if got.Decisions[0].At.IsZero() {
		t.Fatal("decision time missing")
	}
}

func TestAwaitReturnsDecisionAndTimesOut(t *testing.T) {
	store := review.NewMemory()
	item := openItem(t, store)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := store.Await(ctx, item.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("await err = %v; want deadline", err)
	}

	// Once decided, Await returns the latest decision immediately.
	want, err := store.Decide(context.Background(), item.ID, review.Decision{
		Reviewer: "u", Role: "tenant_admin", Result: json.RawMessage(`{"verdict":"accept"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Await(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reviewer != want.Decisions[0].Reviewer {
		t.Fatalf("await = %+v", got)
	}
}

func TestListFiltersAndPaginates(t *testing.T) {
	store := review.NewMemory()
	openItem(t, store)
	second, err := store.Create(context.Background(), review.Item{
		RunID: "run-2", Subject: "claim-2", Result: json.RawMessage(`{"verdict":"reject"}`),
		Schema: json.RawMessage(resultSchema), Role: "reviewer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Decide(context.Background(), second.ID, review.Decision{
		Reviewer: "u", Role: "reviewer", Result: json.RawMessage(`{"verdict":"reject"}`),
	}); err != nil {
		t.Fatal(err)
	}
	items, total, err := store.List(context.Background(), review.StatusOpen, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(items) != 1 || items[0].Status != review.StatusOpen {
		t.Fatalf("open items = %+v total = %d", items, total)
	}
	items, total, err = store.List(context.Background(), "", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 1 {
		t.Fatalf("page = %+v total = %d", items, total)
	}
}

func TestSampledPolicy(t *testing.T) {
	policy := func(result services.Outcome) bool { return result.ToolCalls > 0 }
	sampled := review.Sampled(policy, 0.5, 0.3)
	if !sampled(services.Outcome{ToolCalls: 1}) {
		t.Fatal("policy true must always enqueue")
	}
	if !sampled(services.Outcome{}) {
		t.Fatal("sample below rate must enqueue")
	}
	if review.Sampled(policy, 0.5, 0.7)(services.Outcome{}) {
		t.Fatal("sample above rate must not enqueue")
	}
}

func TestExpireMarksStaleItems(t *testing.T) {
	store := review.NewMemory()
	item := openItem(t, store)
	// A zero duration makes every open item stale.
	if got := store.Expire(0); got != 1 {
		t.Fatalf("expired = %d; want 1", got)
	}
	got, err := store.Get(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != review.StatusExpired {
		t.Fatalf("status = %q", got.Status)
	}
}

func TestApprovalAnswerCreatesItemAndWaits(t *testing.T) {
	store := review.NewMemory()
	answer := review.ApprovalAnswer(store, "run-9", "reviewer")

	call := services.ToolInvocation{Name: "send_email", Arguments: json.RawMessage(`{"to":"a@example.com"}`)}
	type verdict struct {
		Approved bool `json:"approved"`
	}
	go func() {
		// The reviewer's decision lands after the ask has created its item.
		for range 100 {
			items, _, err := store.List(context.Background(), review.StatusOpen, 10, 0)
			if err == nil && len(items) > 0 {
				encoded, _ := json.Marshal(verdict{Approved: true})
				_, _ = store.Decide(context.Background(), items[0].ID, review.Decision{
					Reviewer: "r1", Role: "reviewer", Result: encoded,
				})
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	approved, err := answer(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if !approved {
		t.Fatal("approved decision must approve the ask")
	}

	// The item records what was asked, so the inbox is an audit trail too.
	items, total, err := store.List(context.Background(), "", 10, 0)
	if err != nil || total != 1 {
		t.Fatalf("items = %+v err = %v", items, err)
	}
	if items[0].Subject != "approval:send_email" || items[0].RunID != "run-9" {
		t.Fatalf("item = %+v", items[0])
	}
}

func TestApprovalAnswerRejectsOnDenial(t *testing.T) {
	store := review.NewMemory()
	answer := review.ApprovalAnswer(store, "run-1", "reviewer")
	go func() {
		for range 100 {
			items, _, err := store.List(context.Background(), review.StatusOpen, 10, 0)
			if err == nil && len(items) > 0 {
				_, _ = store.Decide(context.Background(), items[0].ID, review.Decision{
					Reviewer: "r1", Role: "reviewer", Result: json.RawMessage(`{"approved":false}`),
				})
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	approved, err := answer(context.Background(), services.ToolInvocation{Name: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Fatal("a false verdict must reject the ask")
	}
}

func TestApprovalAnswerTimesOut(t *testing.T) {
	store := review.NewMemory()
	answer := review.ApprovalAnswer(store, "run-1", "reviewer")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := answer(ctx, services.ToolInvocation{Name: "deploy"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v; want deadline", err)
	}
}
