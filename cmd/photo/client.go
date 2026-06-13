package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	ID        string `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	IsAdmin   bool   `json:"isAdmin"`
}

func (c *client) register(ctx context.Context, username, firstName, lastName, password, inviteToken string) (*authResponse, error) {
	body := map[string]string{
		"username": username,
	}
	if firstName != "" {
		body["firstName"] = firstName
	}
	if lastName != "" {
		body["lastName"] = lastName
	}
	body["password"] = password
	if inviteToken != "" {
		body["inviteToken"] = inviteToken
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
	CameraMake    string     `json:"cameraMake"`
	CameraModel   string     `json:"cameraModel"`
	LensModel     string     `json:"lensModel"`
	FocalLength   string     `json:"focalLength"`
	Aperture      string     `json:"aperture"`
	ShutterSpeed  string     `json:"shutterSpeed"`
	ISO           int        `json:"iso"`
	GPSLat        *float64   `json:"gpsLat,omitempty"`
	GPSLon        *float64   `json:"gpsLon,omitempty"`
	CapturedAt    *time.Time `json:"capturedAt"`
	LocationName  string     `json:"locationName"`
	IsRaw         bool       `json:"isRaw"`
	Published     bool       `json:"published"`
	Tags          []tagJSON  `json:"tags"`
	Description   string     `json:"description"`
	EXIFRaw       string     `json:"exifRaw,omitempty"`
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

// checkExists asks the server if a file with the given SHA-256 is already
// in the library. Returns (true, id) if found, (false, "") if not.
func (c *client) checkExists(ctx context.Context, sha256 string) (bool, string, error) {
	var result struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/photos/exists?sha256="+sha256, nil, &result)
	if err == nil {
		return true, result.ID, nil
	}
	// ENOTFOUND means not in library — not an error for our purposes.
	if isNotFound(err) {
		return false, "", nil
	}
	return false, "", err
}

func (c *client) uploadPhoto(ctx context.Context, filePath string, rawOnly bool) (*photoJSON, error) {
	return c.uploadPhotoOpts(ctx, filePath, rawOnly, false, nil)
}

// uploadPhotoOpts is the full upload implementation used by both add and publish.
// published=true marks the photo as publicly visible.
// tags are attached after upload if non-empty.
func (c *client) uploadPhotoOpts(ctx context.Context, filePath string, rawOnly, published bool, tags []string) (*photoJSON, error) {
	// Pre-flight: compute SHA-256 and check if already in library.
	sha256, err := hashFileHex(filePath)
	if err != nil {
		return nil, fmt.Errorf("hash %q: %w", filePath, err)
	}
	exists, existingID, err := c.checkExists(ctx, sha256)
	if err != nil {
		return nil, fmt.Errorf("pre-flight check: %w", err)
	}
	if exists {
		return nil, &alreadyExistsError{id: existingID}
	}

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
	params := url.Values{}
	if rawOnly {
		params.Set("raw_only", "true")
	}
	if published {
		params.Set("published", "true")
	}
	if len(params) > 0 {
		uploadURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &body)
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

	// Attach tags after successful upload.
	for _, tag := range tags {
		if err := c.attachTag(ctx, p.ID, tag); err != nil {
			// Non-fatal: log but don't fail the upload.
			fmt.Fprintf(os.Stderr, "  warn   tag %q: %v\n", tag, err)
		}
	}

	return &p, nil
}

// alreadyExistsError is returned by uploadPhotoOpts when the pre-flight check
// finds the file is already in the library.
type alreadyExistsError struct{ id string }

func (e *alreadyExistsError) Error() string {
	return fmt.Sprintf("already in library (id: %s)", e.id)
}

// isAlreadyExists reports whether err is an alreadyExistsError.
func isAlreadyExists(err error) bool {
	_, ok := err.(*alreadyExistsError)
	return ok
}

// isNotFound reports whether an API error response has code "not_found".
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "code: not_found")
}



func (c *client) getStatus(ctx context.Context) (*statusJSON, error) {
	var st statusJSON
	if err := c.do(ctx, http.MethodGet, "/api/v1/status", nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (c *client) getAdminStatus(ctx context.Context) (*statusJSON, error) {
	var st statusJSON
	if err := c.do(ctx, http.MethodGet, "/api/v1/admin/status", nil, &st); err != nil {
		return nil, err
	}
	return &st, nil
}


func (c *client) getPhoto(ctx context.Context, id string) (*photoJSON, error) {
	var p photoJSON
	if err := c.do(ctx, http.MethodGet, "/api/v1/photos/"+id, nil, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *client) updatePhoto(ctx context.Context, id, description, location string, published *bool) (*photoJSON, error) {
	body := map[string]interface{}{}
	if description != "" {
		body["description"] = description
	}
	if location != "" {
		body["locationName"] = location
	}
	if published != nil {
		body["published"] = *published
	}
	var p photoJSON
	if err := c.do(ctx, http.MethodPatch, "/api/v1/photos/"+id, body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *client) regeocode(ctx context.Context, id, location string) (*photoJSON, error) {
	var body interface{}
	if location != "" {
		body = map[string]string{"locationName": location}
	}
	var p photoJSON
	if err := c.do(ctx, http.MethodPost, "/api/v1/photos/"+id+"/regeocode", body, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

type regeocodeMissingResult struct {
	Total    int `json:"total"`
	Updated  int `json:"updated"`
	Failures []struct {
		PhotoID string `json:"photoId"`
		Error   string `json:"error"`
	} `json:"failures"`
}

func (c *client) regeocodeMissing(ctx context.Context) (*regeocodeMissingResult, error) {
	var result regeocodeMissingResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/photos/regeocode-missing", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}


func (c *client) deletePhoto(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/photos/"+id, nil, nil)
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

// errUnauthorized is returned by checkStatus when the server responds 401.
// Callers can check for this to give a specific "please log in again" message.
var errUnauthorized = fmt.Errorf("session expired or invalid — run 'photo login' to authenticate")

// checkStatus returns a descriptive error if the HTTP response is not 2xx.
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
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

// hashFileHex computes the SHA-256 hex digest of a file.
func hashFileHex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func openFile(path string) (*os.File, error) {
	return os.Open(path)
}

// --- Invites ------------------------------------------------------------

type inviteJSON struct {
	ID        string  `json:"id"`
	Token     string  `json:"token"`
	CreatedBy string  `json:"createdBy"`
	CreatedAt string  `json:"createdAt"`
	ExpiresAt string  `json:"expiresAt"`
	UsedAt    *string `json:"usedAt,omitempty"`
	UsedBy    *string `json:"usedBy,omitempty"`
}

func (c *client) inviteCreate(ctx context.Context, ttlHours int) (*inviteJSON, error) {
	body := map[string]int{"ttlHours": ttlHours}
	var resp inviteJSON
	if err := c.do(ctx, http.MethodPost, "/api/v1/admin/invites", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *client) inviteList(ctx context.Context) ([]inviteJSON, error) {
	var resp struct {
		Invites []inviteJSON `json:"invites"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/admin/invites", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Invites, nil
}

func (c *client) inviteRevoke(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodDelete, "/api/v1/admin/invites/"+token, nil, nil)
}

// --- Backup ---------------------------------------------------------------

// backup downloads the database backup and writes it to w.
func (c *client) backup(ctx context.Context, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/backup", nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}

	_, err = io.Copy(w, resp.Body)
	return err
}
