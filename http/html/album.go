package html

import (
	"net/http"
	"strconv"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

func (s *Server) handleAlbumList(w http.ResponseWriter, r *http.Request) {
	userID, authed := s.authenticatedUserID(r)

	filter := photo.AlbumFilter{Limit: 200}
	if authed {
		filter.UserID = userID
	} else {
		// Public album list — only show albums that have at least one published photo.
		// For simplicity at MVP: show all albums. Fine for a personal library.
		// TODO: filter to albums containing published photos only.
	}

	albums, total, err := s.AlbumService.FindAlbums(r.Context(), filter)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	s.render(w, r, "albums.html", struct {
		baseData
		Albums []*photo.Album
		Total  int
	}{
		baseData: s.newBase(r, "albums"),
		Albums:   albums,
		Total:    total,
	})
}

func (s *Server) handleAlbumDetail(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("id")
	albumID, err := kid.FromString(raw)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	album, err := s.AlbumService.FindAlbumByID(r.Context(), albumID)
	if err != nil {
		if photo.ErrorCode(err) == photo.ENOTFOUND {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	photos, total, err := s.AlbumService.FindAlbumPhotos(r.Context(), albumID, offset, pageSize)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
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

	s.render(w, r, "album.html", struct {
		baseData
		Album    *photo.Album
		Photos   []*photo.Photo
		Total    int
		PrevPage string
		NextPage string
		PageInfo string
	}{
		baseData: s.newBase(r, "album"),
		Album:    album,
		Photos:   photos,
		Total:    total,
		PrevPage: prevPage,
		NextPage: nextPage,
		PageInfo: pageInfo(offset, pageSize, total),
	})
}
