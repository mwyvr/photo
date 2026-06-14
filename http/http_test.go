package http_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mwyvr/kid"
	photohttp "github.com/mwyvr/photo/http"
	"github.com/mwyvr/photo/mock"
)

// testServer wires a Server with mock services and returns it plus a test HTTP server.
func testServer(t *testing.T) (*photohttp.Server, *httptest.Server) {
	t.Helper()
	secret := []byte("test-secret-32-bytes-xxxxxxxxxx!!")

	srv := photohttp.New("127.0.0.1:0")
	srv.JWTSecret = secret
	srv.UserService = mock.NewUserService()
	srv.SessionService = mock.NewSessionService()
	srv.PhotoService = mock.NewPhotoService()
	srv.TagService = mock.NewTagService()
	srv.StatusService = &mock.StatusService{}
	srv.AlbumService = mock.NewAlbumService()
	srv.BackupService = &mock.BackupService{}
	srv.InviteService = mock.NewInviteService()
	srv.LibraryRoot = t.TempDir()
	srv.HouseholdMode = true

	ts := httptest.NewServer(photohttp.WrapForTest(srv.Router()))
	t.Cleanup(ts.Close)
	return srv, ts
}

// registerAndLogin creates a user and returns a valid Bearer token.
// username may be a bare name (e.g. "alice") — it will be used as the
// local part of the email-format username (e.g. "alice@example.com").
func registerAndLogin(t *testing.T, ts *httptest.Server, username, password string) string {
	t.Helper()

	email := username
	if !strings.Contains(email, "@") {
		email = username + "@example.com"
	}

	body, _ := json.Marshal(map[string]string{
		"username": email,
		"password": password,
	})
	resp, err := ts.Client().Post(ts.URL+"/api/v1/register",
		"application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status = %d, want 201", resp.StatusCode)
	}

	var result struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&result) //nolint
	if result.Token == "" {
		t.Fatal("expected token in register response")
	}
	return result.Token
}

func authHeader(token string) string {
	return "Bearer " + token
}

// --- Auth tests -------------------------------------------------------------

func TestRegister(t *testing.T) {
	_, ts := testServer(t)

	body, _ := json.Marshal(map[string]string{
		"username": "alice@example.com", "password": "password123",
	})
	resp, err := ts.Client().Post(ts.URL+"/api/v1/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result) //nolint
	if result["token"] == nil {
		t.Error("expected token in response")
	}
}

func TestRegister_MissingFields(t *testing.T) {
	_, ts := testServer(t)

	body, _ := json.Marshal(map[string]string{"username": "alice"})
	resp, _ := ts.Client().Post(ts.URL+"/api/v1/register", "application/json", bytes.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	_, ts := testServer(t)
	registerAndLogin(t, ts, "alice", "password123")

	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "wrongpassword"})
	resp, _ := ts.Client().Post(ts.URL+"/api/v1/login", "application/json", bytes.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRequireAuth_NoToken(t *testing.T) {
	_, ts := testServer(t)

	resp, _ := ts.Client().Get(ts.URL + "/api/v1/photos")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRequireAuth_InvalidToken(t *testing.T) {
	_, ts := testServer(t)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/photos", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.token")
	resp, _ := ts.Client().Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// --- Photo tests ------------------------------------------------------------

func TestListPhotos_Empty(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/photos", nil)
	req.Header.Set("Authorization", authHeader(token))
	resp, _ := ts.Client().Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result) //nolint
	if result["total"].(float64) != 0 {
		t.Errorf("total = %v, want 0", result["total"])
	}
}

func TestGetPhoto_NotFound(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	id := kid.New().String()
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/photos/"+id, nil)
	req.Header.Set("Authorization", authHeader(token))
	resp, _ := ts.Client().Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetPhoto_InvalidID(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/photos/not-a-valid-id", nil)
	req.Header.Set("Authorization", authHeader(token))
	resp, _ := ts.Client().Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPhotoExists_NotFound(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/photos/exists?sha256=nonexistent", nil)
	req.Header.Set("Authorization", authHeader(token))
	resp, _ := ts.Client().Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAttachDetachTag(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	// We need a photo in the mock service. Add one directly.
	// This tests the tag endpoints in isolation.
	// A full integration test would upload first.

	// Attach tag to non-existent photo returns 404.
	id := kid.New().String()
	req, _ := http.NewRequest(http.MethodPost,
		ts.URL+"/api/v1/photos/"+id+"/tags/travel", nil)
	req.Header.Set("Authorization", authHeader(token))
	resp, _ := ts.Client().Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("attach to nonexistent photo: status = %d, want 404", resp.StatusCode)
	}
}

// --- Security header tests --------------------------------------------------

func TestSecurityHeaders(t *testing.T) {
	_, ts := testServer(t)

	resp, err := ts.Client().Get(ts.URL + "/api/v1/photos")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	headers := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	}
	for h, want := range headers {
		if got := resp.Header.Get(h); got != want {
			t.Errorf("header %s = %q, want %q", h, got, want)
		}
	}
	csp := resp.Header.Get("Content-Security-Policy")
	if csp == "" {
		t.Error("expected Content-Security-Policy header to be set")
	}
	if resp.Header.Get("Server") == "" {
		t.Error("expected Server header to be set")
	}
}

// --- Rate limiting tests ----------------------------------------------------

func TestRateLimit_BlocksAfterFailures(t *testing.T) {
	_, ts := testServer(t)

	// Register a user so login attempts hit the bcrypt path.
	registerAndLogin(t, ts, "alice", "correctpassword")

	loginWithWrongPassword := func() int {
		body, _ := json.Marshal(map[string]string{
			"username": "alice", "password": "wrong",
		})
		resp, _ := ts.Client().Post(ts.URL+"/api/v1/login",
			"application/json", bytes.NewReader(body))
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// First 5 failures should return 401.
	for i := 0; i < 5; i++ {
		if status := loginWithWrongPassword(); status != http.StatusUnauthorized {
			t.Errorf("attempt %d: status = %d, want 401", i+1, status)
		}
	}

	// 6th attempt should be rate limited — 429.
	if status := loginWithWrongPassword(); status != http.StatusTooManyRequests {
		t.Errorf("6th attempt: status = %d, want 429 (rate limited)", status)
	}
}

// --- Login timing test ------------------------------------------------------

func TestLogin_UnknownUsername_Returns401(t *testing.T) {
	_, ts := testServer(t)

	// An unknown username should return 401, not 404,
	// to prevent username enumeration.
	body, _ := json.Marshal(map[string]string{
		"username": "doesnotexist", "password": "somepassword",
	})
	resp, _ := ts.Client().Post(ts.URL+"/api/v1/login",
		"application/json", bytes.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (not 404, to prevent enumeration)", resp.StatusCode)
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result) //nolint
	// Error message should not distinguish between bad username and bad password.
	msg, _ := result["error"].(string)
	if msg == "" {
		t.Error("expected error message in response")
	}
}

// --- Album slug tests -------------------------------------------------------

func TestAlbumCreate_HasSlug(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	body, _ := json.Marshal(map[string]string{"name": "France 2024"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/albums",
		bytes.NewReader(body))
	req.Header.Set("Authorization", authHeader(token))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := ts.Client().Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var album map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&album) //nolint
	slug, _ := album["slug"].(string)
	if slug != "france-2024" {
		t.Errorf("slug = %q, want %q", slug, "france-2024")
	}
}

func TestAlbumFetch_BySlug(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	// Create album.
	body, _ := json.Marshal(map[string]string{"name": "Travel"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/albums",
		bytes.NewReader(body))
	req.Header.Set("Authorization", authHeader(token))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := ts.Client().Do(req)
	resp.Body.Close()

	// Fetch by slug.
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/albums/travel", nil)
	req2.Header.Set("Authorization", authHeader(token))
	resp2, _ := ts.Client().Do(req2)
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Errorf("fetch by slug: status = %d, want 200", resp2.StatusCode)
	}
}


func TestStatus_Authenticated(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/status", nil)
	req.Header.Set("Authorization", authHeader(token))
	resp, _ := ts.Client().Do(req)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestStatus_Unauthenticated(t *testing.T) {
	_, ts := testServer(t)

	resp, _ := ts.Client().Get(ts.URL + "/api/v1/status")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// --- Backup tests ------------------------------------------------------------

func TestBackup_Authenticated(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/backup", nil)
	req.Header.Set("Authorization", authHeader(token))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".db") {
		t.Errorf("Content-Disposition = %q, want attachment with .db filename", cd)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Error("expected non-empty backup body")
	}
}

func TestBackup_Unauthenticated(t *testing.T) {
	_, ts := testServer(t)

	resp, _ := ts.Client().Get(ts.URL + "/api/v1/backup")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// --- Admin / invite tests -----------------------------------------------------

func TestFirstUser_IsAdmin(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/backup", nil)
	req.Header.Set("Authorization", authHeader(token))
	resp, _ := ts.Client().Do(req)
	defer resp.Body.Close()

	// First user is admin, so backup (admin-only) should succeed.
	if resp.StatusCode != http.StatusOK {
		t.Errorf("first user backup access: status = %d, want 200", resp.StatusCode)
	}
}

func TestRegister_SecondUserRequiresInvite(t *testing.T) {
	_, ts := testServer(t)
	registerAndLogin(t, ts, "alice", "password123") // first user, no invite needed

	// Second user without invite token should be rejected.
	body, _ := json.Marshal(map[string]string{
		"username": "bob@example.com", "password": "password123",
	})
	resp, _ := ts.Client().Post(ts.URL+"/api/v1/register", "application/json", bytes.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("second user without invite: status = %d, want 403", resp.StatusCode)
	}
}

func TestRegister_SecondUserWithValidInvite(t *testing.T) {
	_, ts := testServer(t)
	aliceToken := registerAndLogin(t, ts, "alice", "password123")

	// Alice (admin) creates an invite.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/admin/invites",
		bytes.NewReader([]byte(`{"ttlHours":24}`)))
	req.Header.Set("Authorization", authHeader(aliceToken))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create invite: status = %d, want 201", resp.StatusCode)
	}

	var inv struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&inv) //nolint

	// Bob registers using the invite token.
	body, _ := json.Marshal(map[string]string{
		"username": "bob@example.com",
		"password": "password123", "inviteToken": inv.Token,
	})
	resp2, _ := ts.Client().Post(ts.URL+"/api/v1/register", "application/json", bytes.NewReader(body))
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusCreated {
		t.Errorf("register with valid invite: status = %d, want 201", resp2.StatusCode)
	}

	var result struct {
		User struct {
			IsAdmin bool `json:"isAdmin"`
		} `json:"user"`
	}
	json.NewDecoder(resp2.Body).Decode(&result) //nolint
	if result.User.IsAdmin {
		t.Error("second user should not be admin")
	}
}

func TestRegister_InviteCannotBeReused(t *testing.T) {
	_, ts := testServer(t)
	aliceToken := registerAndLogin(t, ts, "alice", "password123")

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/admin/invites",
		bytes.NewReader([]byte(`{"ttlHours":24}`)))
	req.Header.Set("Authorization", authHeader(aliceToken))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := ts.Client().Do(req)
	defer resp.Body.Close()

	var inv struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&inv) //nolint

	register := func(username string) int {
		body, _ := json.Marshal(map[string]string{
			"username": username + "@example.com",
			"password": "password123", "inviteToken": inv.Token,
		})
		r, _ := ts.Client().Post(ts.URL+"/api/v1/register", "application/json", bytes.NewReader(body))
		defer r.Body.Close()
		return r.StatusCode
	}

	if status := register("bob"); status != http.StatusCreated {
		t.Fatalf("first use: status = %d, want 201", status)
	}
	if status := register("carol"); status != http.StatusForbidden {
		t.Errorf("reuse: status = %d, want 403", status)
	}
}

func TestBackup_NonAdminForbidden(t *testing.T) {
	_, ts := testServer(t)
	aliceToken := registerAndLogin(t, ts, "alice", "password123") // admin

	// Create invite, register bob (non-admin).
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/admin/invites",
		bytes.NewReader([]byte(`{"ttlHours":24}`)))
	req.Header.Set("Authorization", authHeader(aliceToken))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := ts.Client().Do(req)
	var inv struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&inv) //nolint
	resp.Body.Close()

	body, _ := json.Marshal(map[string]string{
		"username": "bob@example.com",
		"password": "password123", "inviteToken": inv.Token,
	})
	resp2, _ := ts.Client().Post(ts.URL+"/api/v1/register", "application/json", bytes.NewReader(body))
	var bobAuth struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp2.Body).Decode(&bobAuth) //nolint
	resp2.Body.Close()

	// Bob (non-admin) tries to access backup.
	req3, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/backup", nil)
	req3.Header.Set("Authorization", authHeader(bobAuth.Token))
	resp3, _ := ts.Client().Do(req3)
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusForbidden {
		t.Errorf("non-admin backup access: status = %d, want 403", resp3.StatusCode)
	}
}

// --- Cross-origin protection tests -------------------------------------------

func TestCrossOriginProtection_BlocksCrossSitePost(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	// Simulate a cross-site browser POST: Origin differs from the request's
	// own host, and Sec-Fetch-Site indicates cross-site.
	body, _ := json.Marshal(map[string]string{"description": "hijacked"})
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/photos/06bq7xhnr03mlz6r",
		bytes.NewReader(body))
	req.Header.Set("Authorization", authHeader(token))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site POST: status = %d, want 403 (CrossOriginProtection)", resp.StatusCode)
	}
}

func TestCrossOriginProtection_AllowsNonBrowserRequests(t *testing.T) {
	_, ts := testServer(t)
	token := registerAndLogin(t, ts, "alice", "password123")

	// A request with no Origin/Sec-Fetch-Site header — like the CLI using
	// Bearer token auth — is treated as non-browser and is not subject to
	// CrossOriginProtection.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/status", nil)
	req.Header.Set("Authorization", authHeader(token))

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("non-browser request: status = %d, want 200", resp.StatusCode)
	}
}

func TestCrossOriginProtection_AllowsSameOriginPost(t *testing.T) {
	_, ts := testServer(t)

	// A same-origin browser POST (Sec-Fetch-Site: same-origin) should pass
	// CrossOriginProtection. Use registration, which is unauthenticated.
	body, _ := json.Marshal(map[string]string{
		"username": "sameorigin@example.com", "password": "password123",
	})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("same-origin POST: status = %d, want 201", resp.StatusCode)
	}
}
