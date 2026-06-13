package html

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

const pageSize = 48

func (s *Server) handleGrid(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.renderNotFound(w, r)
		return
	}

	userID, authed := s.authenticatedUserID(r)
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))

	filter := photo.PhotoFilter{
		Limit:  pageSize,
		Offset: offset,
	}
	scopeAll := q.Get("scope") == "all"
	if authed {
		filter.UserID = userID
		filter.IncludeOthersPublished = scopeAll
	} else {
		t := true
		filter.Published = &t
	}

	if v := q.Get("location"); v != "" {
		filter.Location = &v
	}
	if v := q.Get("tag"); v != "" {
		filter.Tags = []string{v}
	}
	if v := q.Get("after"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.CapturedAfter = &t
		}
	}
	if v := q.Get("before"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24*time.Hour - time.Second)
			filter.CapturedBefore = &end
		}
	}

	photos, total, err := s.PhotoService.FindPhotos(r.Context(), filter)
	if err != nil {
		s.renderServerError(w, r, err)
		return
	}

	type queryState struct {
		Location string
		Tag      string
		After    string
		Before   string
		Scope    string
		Active   bool
	}
	qs := queryState{
		Location: q.Get("location"),
		Tag:      q.Get("tag"),
		After:    q.Get("after"),
		Before:   q.Get("before"),
		Scope:    q.Get("scope"),
	}
	qs.Active = qs.Location != "" || qs.Tag != "" || qs.After != "" || qs.Before != ""

	// Build a context query string that detail pages use to resolve prev/next.
	ctxParams := url.Values{}
	ctxParams.Set("ctx", "grid")
	if qs.Location != "" {
		ctxParams.Set("location", qs.Location)
	}
	if qs.Tag != "" {
		ctxParams.Set("tag", qs.Tag)
	}
	if qs.After != "" {
		ctxParams.Set("after", qs.After)
	}
	if qs.Before != "" {
		ctxParams.Set("before", qs.Before)
	}
	if qs.Scope != "" {
		ctxParams.Set("scope", qs.Scope)
	}
	if offset > 0 {
		ctxParams.Set("offset", strconv.Itoa(offset))
	}
	ctxQuery := ctxParams.Encode()

	// Scope toggle links preserve other filters but reset pagination and
	// switch the scope param.
	scopeMineParams := url.Values{}
	scopeAllParams := url.Values{}
	for _, p := range []url.Values{scopeMineParams, scopeAllParams} {
		if qs.Location != "" {
			p.Set("location", qs.Location)
		}
		if qs.Tag != "" {
			p.Set("tag", qs.Tag)
		}
		if qs.After != "" {
			p.Set("after", qs.After)
		}
		if qs.Before != "" {
			p.Set("before", qs.Before)
		}
	}
	scopeAllParams.Set("scope", "all")

	var prevPage, nextPage string
	if offset > 0 {
		prev := offset - pageSize
		if prev < 0 {
			prev = 0
		}
		prevPage = pageURL(r, prev)
	}
	if offset+pageSize < total {
		nextPage = pageURL(r, offset+pageSize)
	}

	s.render(w, r, "grid.html", struct {
		baseData
		Photos         []*photo.Photo
		Total          int
		Query          queryState
		CtxQuery       template.URL
		PrevPage       string
		NextPage       string
		PageInfo       string
		ScopeMineQuery template.URL
		ScopeAllQuery  template.URL
	}{
		baseData:       s.newBase(r, "grid"),
		Photos:         photos,
		Total:          total,
		Query:          qs,
		CtxQuery:       template.URL(ctxQuery),
		PrevPage:       prevPage,
		NextPage:       nextPage,
		PageInfo:       pageInfo(offset, pageSize, total),
		ScopeMineQuery: template.URL(scopeMineParams.Encode()),
		ScopeAllQuery:  template.URL(scopeAllParams.Encode()),
	})
}

func (s *Server) handlePhotoDetail(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")
	id, err := kid.FromString(raw)
	if err != nil {
		s.renderNotFound(w, r)
		return
	}

	p, err := s.PhotoService.FindPhotoByID(r.Context(), id)
	if err != nil {
		if photo.ErrorCode(err) == photo.ENOTFOUND {
			s.renderNotFound(w, r)
		} else {
			s.renderServerError(w, r, err)
		}
		return
	}

	userID, authed := s.authenticatedUserID(r)
	if !p.Published && !authed {
		s.renderNotFound(w, r)
		return
	}

	prevURL, nextURL, backURL := s.resolveNavigation(r, id, userID, authed)

	// Resolve the uploader's display name for authenticated viewers.
	// Shown regardless of whether it's the viewer's own photo, as a simple
	// confirmation of ownership in a shared library.
	var uploaderName string
	if authed {
		if u, err := s.UserService.FindUserByID(r.Context(), p.UserID); err == nil {
			uploaderName = u.DisplayName()
		}
	}

	s.render(w, r, "detail.html", struct {
		baseData
		Photo        *photo.Photo
		PrevURL      string
		NextURL      string
		BackURL      string
		UploaderName string
	}{
		baseData:     s.newBase(r, "detail"),
		Photo:        p,
		PrevURL:      prevURL,
		NextURL:      nextURL,
		BackURL:      backURL,
		UploaderName: uploaderName,
	})
}

// resolveNavigation determines prev/next photo URLs based on the context
// query params passed from the grid or album view.
func (s *Server) resolveNavigation(r *http.Request, currentID kid.ID, userID kid.ID, authed bool) (prevURL, nextURL, backURL string) {
	q := r.URL.Query()
	ctx := q.Get("ctx")

	ctxParams := url.Values{}
	for k, v := range q {
		ctxParams[k] = v
	}

	switch ctx {
	case "grid":
		backURL = buildBackURL("/", q)
		prevURL, nextURL = s.adjacentInGrid(r, currentID, userID, authed, q, ctxParams)
	case "album":
		albumIDStr := q.Get("album")
		backURL = "/albums/" + albumIDStr
		prevURL, nextURL = s.adjacentInAlbum(r, currentID, authed, albumIDStr, q, ctxParams)
	default:
		backURL = "javascript:history.back()"
	}
	return
}

// adjacentInGrid finds the prev/next photo IDs within the current grid filter context.
func (s *Server) adjacentInGrid(r *http.Request, currentID kid.ID, userID kid.ID, authed bool, q url.Values, ctxParams url.Values) (prevURL, nextURL string) {
	offset, _ := strconv.Atoi(q.Get("offset"))

	filter := photo.PhotoFilter{
		Limit:  pageSize,
		Offset: offset,
	}
	if authed {
		filter.UserID = userID
		filter.IncludeOthersPublished = q.Get("scope") == "all"
	} else {
		t := true
		filter.Published = &t
	}
	if v := q.Get("location"); v != "" {
		filter.Location = &v
	}
	if v := q.Get("tag"); v != "" {
		filter.Tags = []string{v}
	}
	if v := q.Get("after"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			filter.CapturedAfter = &t
		}
	}
	if v := q.Get("before"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			end := t.Add(24*time.Hour - time.Second)
			filter.CapturedBefore = &end
		}
	}

	photos, total, err := s.PhotoService.FindPhotos(r.Context(), filter)
	if err != nil {
		return
	}

	// Find current photo's position within this page.
	pos := -1
	for i, ph := range photos {
		if ph.ID == currentID {
			pos = i
			break
		}
	}
	if pos == -1 {
		return // photo not in this context
	}

	// Previous photo.
	if pos > 0 {
		prevURL = detailURL(photos[pos-1].ID, ctxParams)
	} else if offset > 0 {
		// Fetch the last photo from the previous page.
		prevOffset := offset - pageSize
		if prevOffset < 0 {
			prevOffset = 0
		}
		prevFilter := filter
		prevFilter.Offset = prevOffset
		prevFilter.Limit = pageSize
		prevPhotos, _, err := s.PhotoService.FindPhotos(r.Context(), prevFilter)
		if err == nil && len(prevPhotos) > 0 {
			pp := url.Values{}
			for k, v := range ctxParams {
				pp[k] = v
			}
			pp.Set("offset", strconv.Itoa(prevOffset))
			prevURL = detailURL(prevPhotos[len(prevPhotos)-1].ID, pp)
		}
	}

	// Next photo.
	if pos < len(photos)-1 {
		nextURL = detailURL(photos[pos+1].ID, ctxParams)
	} else if offset+pageSize < total {
		// Fetch the first photo from the next page.
		nextOffset := offset + pageSize
		nextFilter := filter
		nextFilter.Offset = nextOffset
		nextFilter.Limit = 1
		nextPhotos, _, err := s.PhotoService.FindPhotos(r.Context(), nextFilter)
		if err == nil && len(nextPhotos) > 0 {
			np := url.Values{}
			for k, v := range ctxParams {
				np[k] = v
			}
			np.Set("offset", strconv.Itoa(nextOffset))
			nextURL = detailURL(nextPhotos[0].ID, np)
		}
	}
	return
}

// adjacentInAlbum finds the prev/next photo IDs within an album.
func (s *Server) adjacentInAlbum(r *http.Request, currentID kid.ID, authed bool, albumIDStr string, q url.Values, ctxParams url.Values) (prevURL, nextURL string) {
	// Resolve album by slug or kid ID.
	album, err := s.AlbumService.FindAlbumBySlug(r.Context(), albumIDStr)
	if err != nil {
		if id, idErr := kid.FromString(albumIDStr); idErr == nil {
			album, err = s.AlbumService.FindAlbumByID(r.Context(), id)
		}
	}
	if err != nil {
		return
	}
	albumID := album.ID
	offset, _ := strconv.Atoi(q.Get("offset"))

	photos, total, err := s.AlbumService.FindAlbumPhotos(r.Context(), albumID, offset, pageSize)
	if err != nil {
		return
	}

	// Filter unpublished for public visitors.
	if !authed {
		var pub []*photo.Photo
		for _, p := range photos {
			if p.Published {
				pub = append(pub, p)
			}
		}
		photos = pub
		total = len(pub)
	}

	pos := -1
	for i, ph := range photos {
		if ph.ID == currentID {
			pos = i
			break
		}
	}
	if pos == -1 {
		return
	}

	if pos > 0 {
		prevURL = detailURL(photos[pos-1].ID, ctxParams)
	} else if offset > 0 {
		prevOffset := offset - pageSize
		if prevOffset < 0 {
			prevOffset = 0
		}
		prevPhotos, _, err := s.AlbumService.FindAlbumPhotos(r.Context(), albumID, prevOffset, pageSize)
		if err == nil && len(prevPhotos) > 0 {
			pp := url.Values{}
			for k, v := range ctxParams {
				pp[k] = v
			}
			pp.Set("offset", strconv.Itoa(prevOffset))
			prevURL = detailURL(prevPhotos[len(prevPhotos)-1].ID, pp)
		}
	}

	if pos < len(photos)-1 {
		nextURL = detailURL(photos[pos+1].ID, ctxParams)
	} else if offset+pageSize < total {
		nextOffset := offset + pageSize
		nextPhotos, _, err := s.AlbumService.FindAlbumPhotos(r.Context(), albumID, nextOffset, 1)
		if err == nil && len(nextPhotos) > 0 {
			np := url.Values{}
			for k, v := range ctxParams {
				np[k] = v
			}
			np.Set("offset", strconv.Itoa(nextOffset))
			nextURL = detailURL(nextPhotos[0].ID, np)
		}
	}
	return
}

// detailURL builds a /photo/:id URL with context query params.
func detailURL(id kid.ID, params url.Values) string {
	if len(params) == 0 {
		return "/photo/" + id.String()
	}
	return "/photo/" + id.String() + "?" + params.Encode()
}

// buildBackURL reconstructs the grid URL with the original filter params.
func buildBackURL(base string, q url.Values) string {
	params := url.Values{}
	for _, k := range []string{"location", "tag", "after", "before", "offset"} {
		if v := q.Get(k); v != "" {
			params.Set(k, v)
		}
	}
	if len(params) == 0 {
		return base
	}
	return base + "?" + params.Encode()
}

// handleMe renders the personal status/account page for the logged-in user.
// Shows stats scoped to that user's own photos. Available to any authenticated user.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	userID, _ := s.authenticatedUserID(r)
	st, err := s.StatusService.LibraryStatus(r.Context(), &userID)
	if err != nil {
		s.renderServerError(w, r, err)
		return
	}
	u, err := s.UserService.FindUserByID(r.Context(), userID)
	if err != nil {
		s.renderServerError(w, r, err)
		return
	}
	s.render(w, r, "me.html", struct {
		baseData
		User   *photo.User
		Status interface{}
	}{
		baseData: s.newBase(r, "me"),
		User:     u,
		Status:   st,
	})
}

// handleAdminStatus renders system-wide statistics across all users.
// Admin only — includes the database backup link.
func (s *Server) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.StatusService.LibraryStatus(r.Context(), (*kid.ID)(nil))
	if err != nil {
		s.renderServerError(w, r, err)
		return
	}
	s.render(w, r, "admin_status.html", struct {
		baseData
		Status interface{}
	}{
		baseData: s.newBase(r, "admin_status"),
		Status:   st,
	})
}

// handleAdminUsers renders a list of all registered users. Admin only.
func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.UserService.FindUsers(r.Context())
	if err != nil {
		s.renderServerError(w, r, err)
		return
	}

	// For each user, get their personal photo count for a quick overview.
	type userRow struct {
		*photo.User
		PhotoCount int
	}
	rows := make([]userRow, 0, len(users))
	for _, u := range users {
		uid := u.ID
		st, err := s.StatusService.LibraryStatus(r.Context(), &uid)
		count := 0
		if err == nil {
			count = st.TotalPhotos
		}
		rows = append(rows, userRow{User: u, PhotoCount: count})
	}

	s.render(w, r, "admin_users.html", struct {
		baseData
		Users []userRow
	}{
		baseData: s.newBase(r, "admin_users"),
		Users:    rows,
	})
}

// handleBackup streams a database backup to authenticated web UI users via
// cookie session. Mirrors the API's /api/v1/backup but usable from a browser
// link without needing a Bearer token.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if s.BackupService == nil {
		s.renderServerError(w, r, fmt.Errorf("backup service not configured"))
		return
	}

	filename := fmt.Sprintf("library-%s.db", time.Now().UTC().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Cache-Control", "private, no-store")

	if err := s.BackupService.Backup(r.Context(), w); err != nil {
		log.Printf("backup: %v", err)
	}
}

// uploadFormData holds the data passed to upload.html.
type uploadFormData struct {
	baseData
	Error   string
	Success string
}

// handleUploadForm renders the upload page. Authenticated users only.
func (s *Server) handleUploadForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "upload.html", uploadFormData{
		baseData: s.newBase(r, "upload"),
	})
}

// handleUploadPost handles a file upload from the web form.
// Mirrors the API's POST /api/v1/photos but renders a page rather than JSON,
// and supports a "published" checkbox from the form.
func (s *Server) handleUploadPost(w http.ResponseWriter, r *http.Request) {
	userID, _ := s.authenticatedUserID(r)

	renderError := func(msg string) {
		s.render(w, r, "upload.html", uploadFormData{
			baseData: s.newBase(r, "upload"),
			Error:    msg,
		})
	}

	// Limit upload size to 200 MB, matching the API.
	r.Body = http.MaxBytesReader(w, r.Body, 200<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		renderError("File is too large or the upload was interrupted. Please try again.")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		renderError("Please choose a file to upload.")
		return
	}
	defer file.Close()

	safeFilename := filepath.Base(header.Filename)

	// "published" checkbox: if checked, the form sends "on"; if unchecked,
	// the field is absent entirely. Fall back to server default otherwise.
	var published bool
	if r.FormValue("published") != "" {
		published = true
	} else {
		published = s.PublishDefault
	}

	opts := photo.ImportOptions{
		UserID:    userID,
		Published: published,
	}

	result := s.Importer.ImportReader(r.Context(), file, safeFilename, opts)
	if result.Err != nil {
		renderError(fmt.Sprintf("Upload failed: %v", result.Err))
		return
	}
	if result.Skipped {
		renderError(fmt.Sprintf("Skipped: %s", result.SkipReason))
		return
	}

	// If publishDefault is true but the file turned out to be RAW, correct it
	// — RAW files are never published by default.
	if result.Photo.IsRaw && published && r.FormValue("published") == "" {
		f := false
		if _, err := s.PhotoService.UpdatePhoto(r.Context(), result.Photo.ID, photo.PhotoUpdate{
			Published: &f,
		}); err != nil {
			log.Printf("correct RAW published flag for %s: %v", result.Photo.ID, err)
		}
		result.Photo.Published = false
	}

	s.render(w, r, "upload.html", uploadFormData{
		baseData: s.newBase(r, "upload"),
		Success:  fmt.Sprintf("Uploaded %s.", safeFilename),
	})
}
