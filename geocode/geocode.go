// Package geocode implements photo.Geocoder using the Nominatim reverse
// geocoding API provided by OpenStreetMap. Nominatim is free to use at low
// volume and requires no API key, making it the right default for an MVP.
//
// Nominatim's usage policy requires:
//   - A descriptive User-Agent header (set via the UserAgent field).
//   - A maximum of 1 request per second.
//
// The Geocoder enforces the rate limit automatically using a token bucket.
// For higher volumes, swap in a geocode.Geocoder backed by OpenCage or
// Google Maps — the photo.Geocoder interface is the same.
package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/mwyvr/photo"
)

const (
	nominatimBaseURL = "https://nominatim.openstreetmap.org/reverse"

	// Nominatim's usage policy: max 1 req/s from a single IP.
	minRequestInterval = time.Second
)

// NominatimGeocoder implements photo.Geocoder using the Nominatim API.
type NominatimGeocoder struct {
	// UserAgent is sent as the HTTP User-Agent header.
	// Nominatim's usage policy requires a descriptive value identifying your
	// application. Example: "photo-manager/0.1 (you@example.com)"
	UserAgent string

	// HTTPClient is used for all outbound requests.
	// Defaults to a client with a 10-second timeout if nil.
	HTTPClient *http.Client

	// mu and lastRequest enforce the 1 req/s rate limit.
	mu          sync.Mutex
	lastRequest time.Time
}

// NewNominatimGeocoder returns a new geocoder with sensible defaults.
// userAgent must be a non-empty, descriptive string per Nominatim's policy.
func NewNominatimGeocoder(userAgent string) *NominatimGeocoder {
	return &NominatimGeocoder{
		UserAgent: userAgent,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ReverseGeocode converts a lat/lon pair into a Location by calling Nominatim.
// The returned Location has PhotoID = 0; the caller must set it before persisting.
//
// Returns ENOTFOUND if the coordinates resolve to no known place.
// Returns EINTERNAL on HTTP or parse errors.
func (g *NominatimGeocoder) ReverseGeocode(ctx context.Context, lat, lon float64) (*photo.Location, error) {
	if g.UserAgent == "" {
		return nil, photo.Errorf(photo.EINTERNAL,
			"geocoder: UserAgent must be set (Nominatim usage policy)")
	}

	g.rateLimit()

	params := url.Values{}
	params.Set("lat", fmt.Sprintf("%.8f", lat))
	params.Set("lon", fmt.Sprintf("%.8f", lon))
	params.Set("format", "jsonv2")
	params.Set("zoom", "10") // city level; 14 = suburb, 8 = county
	params.Set("addressdetails", "1")

	reqURL := nominatimBaseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, photo.Errorf(photo.EINTERNAL, "geocoder: build request: %v", err)
	}
	req.Header.Set("User-Agent", g.UserAgent)
	req.Header.Set("Accept-Language", "en") // request English place names

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return nil, photo.Errorf(photo.EINTERNAL, "geocoder: request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, photo.Errorf(photo.ENOTFOUND,
			"geocoder: no place found for (%.6f, %.6f)", lat, lon)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, photo.Errorf(photo.EINTERNAL,
			"geocoder: unexpected status %d for (%.6f, %.6f)", resp.StatusCode, lat, lon)
	}

	var result nominatimResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, photo.Errorf(photo.EINTERNAL, "geocoder: decode response: %v", err)
	}

	// Nominatim returns an error field in JSON for coords that resolve nowhere.
	if result.Error != "" {
		return nil, photo.Errorf(photo.ENOTFOUND,
			"geocoder: %s", result.Error)
	}

	loc := &photo.Location{
		City:        result.bestCity(),
		Region:      result.Address.State,
		Country:     result.Address.Country,
		CountryCode: strings.ToUpper(result.Address.CountryCode),
	}

	return loc, nil
}

// rateLimit blocks until it is safe to make the next Nominatim request,
// enforcing the 1-request-per-second policy.
func (g *NominatimGeocoder) rateLimit() {
	g.mu.Lock()
	defer g.mu.Unlock()

	elapsed := time.Since(g.lastRequest)
	if elapsed < minRequestInterval {
		time.Sleep(minRequestInterval - elapsed)
	}
	g.lastRequest = time.Now()
}

// --- Nominatim JSON response types ------------------------------------------

type nominatimResponse struct {
	// Error is set when Nominatim cannot find a result.
	Error string `json:"error"`

	Address nominatimAddress `json:"address"`
}

type nominatimAddress struct {
	// City-level fields. Nominatim uses different keys depending on the type
	// of place (city, town, village, hamlet). We try each in order.
	City         string `json:"city"`
	Town         string `json:"town"`
	Village      string `json:"village"`
	Hamlet       string `json:"hamlet"`
	Municipality string `json:"municipality"`
	Suburb       string `json:"suburb"`

	// Administrative region (state, province, county, etc.)
	State string `json:"state"`

	// Country.
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
}

// bestCity returns the most specific available populated-place name.
func (r *nominatimResponse) bestCity() string {
	for _, s := range []string{
		r.Address.City,
		r.Address.Town,
		r.Address.Village,
		r.Address.Hamlet,
		r.Address.Municipality,
		r.Address.Suburb,
	} {
		if s != "" {
			return s
		}
	}
	return ""
}
