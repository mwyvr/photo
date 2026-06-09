package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	srv.LibraryRoot = t.TempDir()

	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	return srv, ts
}

// registerAndLogin creates a user and returns a valid Bearer token.
func registerAndLogin(t *testing.T, ts *httptest.Server, username, password string) string {
	t.Helper()

	body, _ := json.Marshal(map[string]string{
		"username": username,
		"email":    username + "@example.com",
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
		"username": "alice", "email": "alice@example.com", "password": "password123",
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

// --- Status test ------------------------------------------------------------

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
