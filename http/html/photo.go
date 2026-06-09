package html

import (
	"net/http"
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

	// Authenticated users see all their photos; public sees published only.
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
		PrevPage string
		NextPage string
		PageInfo string
	}{
		baseData: s.newBase(r, "grid"),
		Photos:   photos,
		Total:    total,
		Query:    qs,
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

	_, authed := s.authenticatedUserID(r)
	if !p.Published && !authed {
		http.NotFound(w, r)
		return
	}

	s.render(w, r, "detail.html", struct {
		baseData
		Photo *photo.Photo
	}{
		baseData: s.newBase(r, "detail"),
		Photo:    p,
	})
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
