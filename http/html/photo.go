package html

import (
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

const pageSize = 48

func (s *Server) handleGrid(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	userID, authed := s.authenticatedUserID(r)
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))

	filter := photo.PhotoFilter{
		Limit:  pageSize,
		Offset: offset,
	}
	if authed {
		filter.UserID = userID
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
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	type queryState struct {
		Location string
		Tag      string
		After    string
		Before   string
		Active   bool
	}
	qs := queryState{
		Location: q.Get("location"),
		Tag:      q.Get("tag"),
		After:    q.Get("after"),
		Before:   q.Get("before"),
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
	if offset > 0 {
		ctxParams.Set("offset", strconv.Itoa(offset))
	}
	ctxQuery := ctxParams.Encode()

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
		Photos   []*photo.Photo
		Total    int
		Query    queryState
		CtxQuery template.URL
		PrevPage string
		NextPage string
		PageInfo string
	}{
		baseData: s.newBase(r, "grid"),
		Photos:   photos,
		Total:    total,
		Query:    qs,
		CtxQuery: template.URL(ctxQuery),
		PrevPage: prevPage,
		NextPage: nextPage,
		PageInfo: pageInfo(offset, pageSize, total),
	})
}

func (s *Server) handlePhotoDetail(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")
	id, err := kid.FromString(raw)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	p, err := s.PhotoService.FindPhotoByID(r.Context(), id)
	if err != nil {
		if photo.ErrorCode(err) == photo.ENOTFOUND {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	userID, authed := s.authenticatedUserID(r)
	if !p.Published && !authed {
		http.NotFound(w, r)
		return
	}

	prevURL, nextURL, backURL := s.resolveNavigation(r, id, userID, authed)

	s.render(w, r, "detail.html", struct {
		baseData
		Photo   *photo.Photo
		PrevURL string
		NextURL string
		BackURL string
	}{
		baseData: s.newBase(r, "detail"),
		Photo:    p,
		PrevURL:  prevURL,
		NextURL:  nextURL,
		BackURL:  backURL,
	})
}

// resolveNavigation determines prev/next photo URLs based on the context
// query params passed from the grid or album view.
func (s *Server) resolveNavigation(r *http.Request, currentID kid.ID, userID kid.ID, authed bool) (prevURL, nextURL, backURL string) {
	q := r.URL.Query()
	ctx := q.Get("ctx")

	log.Printf("nav: id=%s ctx=%q params=%v", currentID, ctx, q)

	// Reconstruct the context query string to pass through to prev/next links.
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

	log.Printf("nav: prev=%q next=%q back=%q", prevURL, nextURL, backURL)
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
	albumID, err := kid.FromString(albumIDStr)
	if err != nil {
		return
	}
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

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.StatusService.LibraryStatus(r.Context())
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	s.render(w, r, "status.html", struct {
		baseData
		Status interface{}
	}{
		baseData: s.newBase(r, "status"),
		Status:   st,
	})
}
