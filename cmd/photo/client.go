package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// client is a thin HTTP client for the photod API.
// All methods return decoded response bodies or a descriptive error.
type client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func newClient(baseURL, token string) *client {
	return &client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// --- auth -------------------------------------------------------------------

type authResponse struct {
	Token string   `json:"token"`
	User  userJSON `json:"user"`
}

type userJSON struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

func (c *client) register(ctx context.Context, username, email, password string) (*authResponse, error) {
	body := map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}
	var resp authResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/register", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *client) login(ctx context.Context, username, password string) (*authResponse, error) {
	body := map[string]string{"username": username, "password": password}
	var resp authResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/login", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *client) logout(ctx context.Context) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/logout", nil, nil)
}

// --- photos -----------------------------------------------------------------

type photoJSON struct {
	ID            string     `json:"id"`
	Filename      string     `json:"filename"`
	StoredPath    string     `json:"storedPath"`
	MIMEType      string     `json:"mimeType"`
	FileType      string     `json:"fileType"`
	FileSizeBytes int64      `json:"fileSizeBytes"`
	CameraModel   string     `json:"cameraModel"`
	CapturedAt    *time.Time `json:"capturedAt"`
	LocationName  string     `json:"locationName"`
	IsRaw         bool       `json:"isRaw"`
	Tags          []tagJSON  `json:"tags"`
	Description   string     `json:"description"`
}

type tagJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type listPhotosResponse struct {
	Photos []photoJSON `json:"photos"`
	Total  int         `json:"total"`
	Offset int         `json:"offset"`
	Limit  int         `json:"limit"`
}

// searchParams collects query parameters for GET /api/v1/photos.
type searchParams struct {
	Location string
	After    string
	Before   string
	Tags     []string
	RawOnly  bool
	Limit    int
	Offset   int
}

func (c *client) listPhotos(ctx context.Context, p searchParams) (*listPhotosResponse, error) {
	q := url.Values{}
	if p.Location != "" {
		q.Set("location", p.Location)
	}
	if p.After != "" {
		q.Set("after", p.After)
	}
	if p.Before != "" {
		q.Set("before", p.Before)
	}
	for _, t := range p.Tags {
		q.Add("tag", t)
	}
	if p.RawOnly {
		q.Set("raw_only", "true")
	}
	if p.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", p.Limit))
	}
	if p.Offset > 0 {
		q.Set("offset", fmt.Sprintf("%d", p.Offset))
	}

	path := "/api/v1/photos"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var resp listPhotosResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *client) uploadPhoto(ctx context.Context, filePath string, rawOnly bool) (*photoJSON, error) {
	f, err := openFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", filePath, err)
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("write file to form: %w", err)
	}
	mw.Close()

	uploadURL := c.baseURL + "/api/v1/photos"
	if rawOnly {
		uploadURL += "?raw_only=true"
	}

	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost, uploadURL, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload request: %w", err)
	}
	defer httpResp.Body.Close()

	if err := checkStatus(httpResp); err != nil {
		return nil, err
	}

	var p photoJSON
	if err := json.NewDecoder(httpResp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("decode upload response: %w", err)
	}
	return &p, nil
}

func (c *client) getPhoto(ctx context.Context, id string) (*photoJSON, error) {
	var p photoJSON
	if err := c.do(ctx, http.MethodGet, "/api/v1/photos/"+id, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *client) attachTag(ctx context.Context, photoID, tagName string) error {
	return c.do(ctx, http.MethodPost,
		fmt.Sprintf("/api/v1/photos/%s/tags/%s", photoID, url.PathEscape(tagName)),
		nil, nil)
}

func (c *client) detachTag(ctx context.Context, photoID, tagName string) error {
	return c.do(ctx, http.MethodDelete,
		fmt.Sprintf("/api/v1/photos/%s/tags/%s", photoID, url.PathEscape(tagName)),
		nil, nil)
}

// --- transport --------------------------------------------------------------

// do executes a JSON request and decodes the response into out (may be nil).
func (c *client) do(ctx context.Context, method, path string, body, out interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s: %w", c.baseURL+path, err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return err
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// checkStatus returns a descriptive error if the HTTP response is not 2xx.
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	// Try to decode an API error body.
	var apiErr struct {
		Message string `json:"error"`
		Code    string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.Message != "" {
		return fmt.Errorf("%s (code: %s)", apiErr.Message, apiErr.Code)
	}
	return fmt.Errorf("server returned %d", resp.StatusCode)
}

// openFile is a thin wrapper around os.Open used by uploadPhoto.
func openFile(path string) (*os.File, error) {
	return os.Open(path)
}
