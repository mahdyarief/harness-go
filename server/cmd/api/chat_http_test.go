package main

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// testChatHTTP covers the chat surface that needs no provider: auth, session
// CRUD, ownership, idempotent create, and the fail-closed path when the LLM is
// disabled. No turn is ever sent to a real endpoint.
//
// Row-level cross-tenant isolation is proven separately against the non-owner
// database role; here the assertion is the HTTP one: a platform admin with no
// session of their own never sees a tenant user's conversation.
func testChatHTTP(t *testing.T, call func(string, string, string, any, ...string) (int, map[string]any), user, platform string) {
	t.Helper()
	const base = "/api/v1/chat/sessions"

	expect := func(method, path, token string, body any, want int, headers ...string) map[string]any {
		t.Helper()
		status, out := call(method, path, token, body, headers...)
		if status != want {
			t.Fatalf("%s %s: %d want %d: %v", method, path, status, want, out)
		}
		return out
	}

	expect(http.MethodGet, base, "", nil, http.StatusUnauthorized)

	// Create is idempotent: the same key replays the first answer rather than
	// opening a second conversation.
	const key = "6f0d3c2e-9d1a-4c8e-9f2b-7a5e4d3c2b1a"
	created := expect(http.MethodPost, base, user, map[string]string{"title": "Percakapan uji"},
		http.StatusCreated, "X-Idempotency-Key", key)
	id, _ := created["data"].(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("create session shape: %v", created)
	}
	if _, err := uuid.Parse(id); err != nil {
		t.Fatalf("session id is not a uuid: %v", id)
	}
	replayed := expect(http.MethodPost, base, user, map[string]string{"title": "Percakapan uji"},
		http.StatusCreated, "X-Idempotency-Key", key)
	if replayed["data"].(map[string]any)["id"] != id {
		t.Fatalf("idempotent create opened a second session: %v", replayed)
	}

	// The title is optional; the server supplies the placeholder the first
	// message is later allowed to replace.
	untitled := expect(http.MethodPost, base, user, map[string]string{}, http.StatusCreated)
	untitledID, _ := untitled["data"].(map[string]any)["id"].(string)
	untitledDetail := expect(http.MethodGet, base+"/"+untitledID, user, nil, http.StatusOK)
	if got := untitledDetail["data"].(map[string]any)["session"].(map[string]any)["title"]; got != "Percakapan baru" {
		t.Fatalf("default title = %v, want Percakapan baru", got)
	}

	list := expect(http.MethodGet, base, user, nil, http.StatusOK)
	if items, ok := list["data"].([]any); !ok || len(items) != 2 {
		t.Fatalf("session list: %v", list)
	}

	detail := expect(http.MethodGet, base+"/"+id, user, nil, http.StatusOK)
	payload, ok := detail["data"].(map[string]any)
	if !ok {
		t.Fatalf("session detail shape: %v", detail)
	}
	if payload["session"].(map[string]any)["title"] != "Percakapan uji" {
		t.Fatalf("session detail: %v", payload)
	}
	if _, ok := payload["messages"].([]any); !ok {
		t.Fatalf("session detail has no messages array: %v", payload)
	}

	// The paged transcript endpoint backs the long-conversation view: it must
	// answer with an envelope carrying offset paging metadata, and it must
	// refuse another owner's session exactly like the detail read does.
	page := expect(http.MethodGet, base+"/"+id+"/messages?offset=0&limit=30", user, nil, http.StatusOK)
	if items, ok := page["data"].([]any); !ok || len(items) != 0 {
		t.Fatalf("messages page shape: %v", page)
	}
	if meta, ok := page["metadata"].(map[string]any); !ok || meta["total"] != float64(0) {
		t.Fatalf("messages page metadata: %v", page)
	}
	expect(http.MethodGet, base+"/not-a-uuid/messages", user, nil, http.StatusBadRequest)
	expect(http.MethodGet, base+"/"+uuid.NewString()+"/messages", user, nil, http.StatusNotFound)
	expect(http.MethodGet, base+"/"+id+"/messages", platform, nil, http.StatusNotFound)

	expect(http.MethodPut, base+"/"+id, user, map[string]string{"title": "Diganti"}, http.StatusOK)
	expect(http.MethodPut, base+"/"+id, user, map[string]string{"title": ""}, http.StatusUnprocessableEntity)
	expect(http.MethodGet, base+"/not-a-uuid", user, nil, http.StatusBadRequest)
	expect(http.MethodGet, base+"/"+uuid.NewString(), user, nil, http.StatusNotFound)

	// Tanpa alokasi token, turn ditolak sebelum memanggil provider.
	turn := expect(http.MethodPost, base+"/"+id+"/turn", user,
		map[string]any{"content": "Halo", "stream": false}, http.StatusPaymentRequired)
	if turn["data"] != nil {
		t.Fatalf("turn tanpa alokasi menghasilkan data: %v", turn)
	}
	tenantID := created["data"].(map[string]any)["tenant_id"].(string)
	expect(http.MethodPost, "/api/v1/platform/tokens/"+tenantID+"/topups", platform,
		map[string]any{"amount": 1000, "note": "Alokasi uji sintetis"}, http.StatusCreated)
	turn = expect(http.MethodPost, base+"/"+id+"/turn", user,
		map[string]any{"content": "Halo", "stream": false}, http.StatusConflict)
	if turn["data"] != nil {
		t.Fatalf("disabled LLM produced data: %v", turn)
	}
	expect(http.MethodPost, base+"/"+id+"/turn", user, map[string]any{"content": ""}, http.StatusUnprocessableEntity)

	// The platform admin's own tenant holds no chat rows, so the tenant user's
	// conversation must not surface in their list or detail read.
	if items, ok := expect(http.MethodGet, base, platform, nil, http.StatusOK)["data"].([]any); !ok || len(items) != 0 {
		t.Fatalf("platform admin sees tenant chat: %v", items)
	}
	expect(http.MethodGet, base+"/"+id, platform, nil, http.StatusNotFound)
	expect(http.MethodDelete, base+"/"+id, platform, nil, http.StatusNotFound)

	expect(http.MethodDelete, base+"/"+id, user, nil, http.StatusOK)
	expect(http.MethodGet, base+"/"+id, user, nil, http.StatusNotFound)
}
