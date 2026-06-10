package html

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

func (s *Server) handleAlbumList(w http.ResponseWriter, r *http.Request) {
	userID, authed := s.authenticatedUserID(r)

	filter := photo.AlbumFilter{Limit: 200}
	if authed {
		filter.UserID = userID
	}

	albums, _, err := s.AlbumService.FindAlbums(r.Context(), filter)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// For unauthenticated visitors, filter to albums that have at least one
	// published photo and correct the photo count to published-only.
	if !authed {
		albums = s.filterPublicAlbums(r, albums)
	}

	s.render(w, r, "albums.html", struct {
		baseData
		Albums []*photo.Album
		Total  int
	}{
		baseData: s.newBase(r, "albums"),
		Albums:   albums,
		Total:    len(albums),
	})
}

// filterPublicAlbums removes albums with no published photos and corrects
// the PhotoCount to reflect only published photos.
func (s *Server) filterPublicAlbums(r *http.Request, albums []*photo.Album) []*photo.Album {
	var out []*photo.Album
	for _, a := range albums {
		all, _, err := s.AlbumService.FindAlbumPhotos(r.Context(), a.ID, 0, 9999)
		if err != nil {
			continue
		}
		count := 0
		for _, p := range all {
			if p.Published {
				count++
			}
		}
		if count > 0 {
			a.PhotoCount = count
			out = append(out, a)
		}
	}
	return out
}

func (s *Server) handleAlbumDetail(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")

	// Try slug first, fall back to kid ID.
	var album *photo.Album
	var err error
	album, err = s.AlbumService.FindAlbumBySlug(r.Context(), raw)
	if err != nil {
		if photo.ErrorCode(err) == photo.ENOTFOUND {
			// Try as kid ID.
			if id, idErr := kid.FromString(raw); idErr == nil {
				album, err = s.AlbumService.FindAlbumByID(r.Context(), id)
			}
		}
	}
	if err != nil {
		if photo.ErrorCode(err) == photo.ENOTFOUND {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	_, authed := s.authenticatedUserID(r)
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	photos, total, err := s.AlbumService.FindAlbumPhotos(r.Context(), album.ID, offset, pageSize)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// For unauthenticated visitors filter out unpublished photos and
	// correct the count so the UI doesn't show empty slots or wrong numbers.
	if !authed {
		var pub []*photo.Photo
		for _, p := range photos {
			if p.Published {
				pub = append(pub, p)
			}
		}
		photos = pub
		total = len(pub)
		// If no published photos at all, return 404.
		if total == 0 {
			http.NotFound(w, r)
			return
		}
	}

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

	// Build context query for detail page navigation.
	ctxParams := url.Values{}
	ctxParams.Set("ctx", "album")
	ctxParams.Set("album", album.Slug)
	if offset > 0 {
		ctxParams.Set("offset", strconv.Itoa(offset))
	}
	ctxQuery := ctxParams.Encode()

	s.render(w, r, "album.html", struct {
		baseData
		Album    *photo.Album
		Photos   []*photo.Photo
		Total    int
		CtxQuery template.URL
		PrevPage string
		NextPage string
		PageInfo string
	}{
		baseData: s.newBase(r, "album"),
		Album:    album,
		Photos:   photos,
		Total:    total,
		CtxQuery: template.URL(ctxQuery),
		PrevPage: prevPage,
		NextPage: nextPage,
		PageInfo: pageInfo(offset, pageSize, total),
	})
}
