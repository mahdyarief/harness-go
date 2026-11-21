package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func testTenantHTTP(t *testing.T, server *fiber.App, db *sql.DB, platform, user, userID string) {
	t.Helper()
	call := func(method, path, token, body string, want int) map[string]any {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := server.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != want {
			t.Fatalf("%s %s: %d want %d: %v", method, path, res.StatusCode, want, out)
		}
		return out
	}
	call(http.MethodGet, "/api/v1/tenants", "", "", 401)

	// The user directory feeds the admin's pickers, so it is super-user only.
	call(http.MethodGet, "/api/v1/users", "", "", 401)
	call(http.MethodGet, "/api/v1/users", user, "", 403)
	directory := call(http.MethodGet, "/api/v1/users?search=example.com", platform, "", 200)
	users, ok := directory["data"].([]any)
	if !ok || len(users) == 0 {
		t.Fatalf("user directory: %v", directory)
	}
	if first, ok := users[0].(map[string]any); !ok || first["id"] == nil || first["email"] == nil || first["role"] == nil {
		t.Fatalf("user directory shape: %v", users[0])
	}
	if meta, ok := directory["metadata"].(map[string]any); !ok || meta["total"] == nil {
		t.Fatalf("user directory meta: %v", directory)
	}
	if got := call(http.MethodGet, "/api/v1/users?search=no-such-user-here", platform, "", 200)["data"].([]any); len(got) != 0 {
		t.Fatalf("user search ignored: %v", got)
	}
	// A syntactically valid but unlisted sort column falls back to the default
	// order instead of reaching the SQL.
	call(http.MethodGet, "/api/v1/users?sorted=image:asc", platform, "", 200)

	body := `{"name":"HTTP tenant","admin_user_id":"` + userID + `"}`
	call(http.MethodPost, "/api/v1/platform/tenants", user, body, 403)
	created := call(http.MethodPost, "/api/v1/platform/tenants", platform, body, 201)
	tenant := created["data"].(map[string]any)["id"].(string)
	path := "/api/v1/tenants/" + tenant + "/members"
	call(http.MethodGet, path, platform, "", 403)
	call(http.MethodPut, path, platform, `{"user_id":"`+userID+`","role":"reviewer"}`, 403)
	call(http.MethodGet, path, user, "", 200)
	listed := call(http.MethodGet, "/api/v1/tenants", user, "", 200)["data"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["role"] != "tenant_admin" {
		t.Fatalf("missing role: %v", listed)
	}
	if got := call(http.MethodGet, "/api/v1/tenants", platform, "", 200)["data"].([]any); len(got) != 0 {
		t.Fatal("platform list bypass")
	}
	call(http.MethodPut, path, user, `{"user_id":"`+userID+`","role":"reviewer"}`, 409)
	call(http.MethodGet, "/api/v1/tenants/not-a-uuid/members", user, "", 400)
	var target string
	if err := db.QueryRow(`SELECT id FROM users WHERE email='stranger@example.com'`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	call(http.MethodPut, path, user, `{"user_id":"`+target+`","role":"reviewer"}`, 200)
	call(http.MethodPut, path, user, `{"user_id":"`+target+`","role":"super_user"}`, 400)
	var platformID string
	if err := db.QueryRow(`SELECT id FROM users WHERE role='super_user'`).Scan(&platformID); err != nil {
		t.Fatal(err)
	}
	other := call(http.MethodPost, "/api/v1/platform/tenants", platform, `{"name":"Other","admin_user_id":"`+platformID+`"}`, 201)["data"].(map[string]any)["id"].(string)
	otherPath := "/api/v1/tenants/" + other + "/members"
	call(http.MethodGet, otherPath, user, "", 403)
	call(http.MethodPut, otherPath, user, `{"user_id":"`+userID+`","role":"tenant_admin"}`, 403)
	call(http.MethodPut, path, user, `{"user_id":"`+platformID+`","role":"reviewer"}`, 200)
	call(http.MethodGet, path, platform, "", 403)
	call(http.MethodPut, path, platform, `{"user_id":"`+platformID+`","role":"tenant_admin"}`, 403)

	testUserHTTP(t, call, db, platform, user, tenant)
}

// testUserHTTP covers the platform admin's create/edit drawer: the account,
// the platform role and the tenant membership are provisioned in one write.
func testUserHTTP(t *testing.T, call func(string, string, string, string, int) map[string]any, db *sql.DB, platform, user, tenant string) {
	t.Helper()
	const password = "admin-set-password-12"
	const email = "drawer-user@example.com"

	call(http.MethodGet, "/api/v1/platform/tenants", "", "", 401)
	call(http.MethodGet, "/api/v1/platform/tenants", user, "", 403)
	directory := call(http.MethodGet, "/api/v1/platform/tenants", platform, "", 200)["data"].([]any)
	if len(directory) == 0 {
		t.Fatal("platform tenant directory is empty")
	}

	call(http.MethodPost, "/api/v1/users", "", ``, 401)
	call(http.MethodPost, "/api/v1/users", user, ``, 403)
	call(http.MethodPost, "/api/v1/users", platform, `{"name":"No Role","email":"x@example.com","password":"`+password+`","role":"approver"}`, 422)
	call(http.MethodPost, "/api/v1/users", platform, `{"name":"Bad Pass","email":"y@example.com","password":"short","role":"user"}`, 422)
	call(http.MethodPost, "/api/v1/users", platform, `{"name":"Half","email":"z@example.com","password":"`+password+`","role":"user","tenant_id":"`+tenant+`"}`, 400)
	call(http.MethodPost, "/api/v1/users", platform, `{"name":"Half","email":"z@example.com","password":"`+password+`","role":"user","tenant_role":"reviewer"}`, 400)

	created := call(http.MethodPost, "/api/v1/users", platform, `{"name":"Drawer User","email":"`+email+`","password":"`+password+`","role":"user","tenant_id":"`+tenant+`","tenant_role":"reviewer"}`, 201)
	data := created["data"].(map[string]any)
	id := data["id"].(string)
	if data["role"] != "user" || data["email"] != email {
		t.Fatalf("created user shape: %v", data)
	}
	call(http.MethodPost, "/api/v1/users", platform, `{"name":"Again","email":"`+email+`","password":"`+password+`","role":"user"}`, 409)

	// The admin-set password is stored hashed on the email account.
	var hashed string
	if err := db.QueryRow(`SELECT a.password FROM authula.accounts a WHERE a.user_id=$1 AND a.provider_id='email'`, id).Scan(&hashed); err != nil || hashed == "" || hashed == password {
		t.Fatalf("admin password not hashed: %q %v", hashed, err)
	}
	var memberRole string
	if err := db.QueryRow(`SELECT role FROM tenant_memberships WHERE user_id=$1 AND tenant_id=$2`, id, tenant).Scan(&memberRole); err != nil || memberRole != "reviewer" {
		t.Fatalf("tenant membership: %q %v", memberRole, err)
	}

	membership := call(http.MethodGet, "/api/v1/users/"+id+"/membership", platform, "", 200)["data"].(map[string]any)
	if membership["tenant_id"] != tenant || membership["role"] != "reviewer" {
		t.Fatalf("membership read: %v", membership)
	}
	call(http.MethodGet, "/api/v1/users/"+id+"/membership", user, "", 403)
	call(http.MethodGet, "/api/v1/users/not-a-uuid/membership", platform, "", 400)

	updated := call(http.MethodPut, "/api/v1/users/"+id, platform, `{"name":"Renamed","email":"`+email+`","password":"","role":"super_user","tenant_id":"`+tenant+`","tenant_role":"tenant_admin"}`, 200)["data"].(map[string]any)
	if updated["name"] != "Renamed" || updated["role"] != "super_user" {
		t.Fatalf("updated user: %v", updated)
	}
	call(http.MethodPut, "/api/v1/users/"+id, user, `{"name":"Nope","email":"`+email+`","role":"user"}`, 403)
	call(http.MethodPut, "/api/v1/users/"+id, platform, `{"name":"Nope","email":"`+email+`","role":"nope"}`, 422)
	call(http.MethodPut, "/api/v1/users/not-a-uuid", platform, `{"name":"Nope","email":"`+email+`","role":"user"}`, 400)
	call(http.MethodPut, "/api/v1/users/"+id, platform, `{"name":"Renamed","email":"stranger@example.com","role":"user"}`, 409)
}
