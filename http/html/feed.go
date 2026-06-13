package html

import (
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/mwyvr/photo"
)

// rssFeed and rssItem model the subset of RSS 2.0 needed for a photo feed.
type rssFeed struct {
	XMLName xml.Name  `xml:"rss"`
	Version string    `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Language    string    `xml:"language,omitempty"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string       `xml:"title"`
	Link        string       `xml:"link"`
	GUID        string       `xml:"guid"`
	PubDate     string       `xml:"pubDate,omitempty"`
	Description string       `xml:"description,omitempty"`
	Enclosure   *rssEnclosure `xml:"enclosure,omitempty"`
}

type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Type   string `xml:"type,attr"`
	Length int64  `xml:"length,attr"`
}

const feedItemLimit = 50

// handleFeed serves an RSS 2.0 feed of the most recently published photos
// across all users — this is a shared library, so the feed isn't scoped to
// any one person.
//
//	GET /feed.xml
//
// Returns 404 if PublicBaseURL is not configured, since absolute URLs are
// required for a valid feed and we have no way to construct them otherwise.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	if s.PublicBaseURL == "" {
		http.NotFound(w, r)
		return
	}

	published := true
	photos, _, err := s.PhotoService.FindPhotos(r.Context(), photo.PhotoFilter{
		Published: &published,
		Limit:     feedItemLimit,
	})
	if err != nil {
		s.renderServerError(w, r, err)
		return
	}

	base := s.PublicBaseURL
	feed := rssFeed{
		Version: "2.0",
		Channel: rssChannel{
			Title:       "Photo Library",
			Link:        base + "/",
			Description: "Recently published photos",
			Language:    "en",
		},
	}

	for _, p := range photos {
		link := fmt.Sprintf("%s/p/%s", base, p.ID)
		thumbURL := fmt.Sprintf("%s/p/%s/thumb", base, p.ID)

		title := p.Filename
		if p.LocationName != "" {
			title = p.LocationName
		}

		desc := p.Description
		if desc == "" && p.LocationName != "" {
			desc = p.LocationName
		}

		var pubDate string
		if p.CapturedAt != nil {
			pubDate = p.CapturedAt.Format(time.RFC1123Z)
		} else {
			pubDate = p.CreatedAt.Format(time.RFC1123Z)
		}

		feed.Channel.Items = append(feed.Channel.Items, rssItem{
			Title:       title,
			Link:        link,
			GUID:        link,
			PubDate:     pubDate,
			Description: desc,
			Enclosure: &rssEnclosure{
				URL:  thumbURL,
				Type: "image/jpeg",
			},
		})
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(xml.Header)) //nolint:errcheck
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		log.Printf("feed: encode: %v", err)
	}
}
