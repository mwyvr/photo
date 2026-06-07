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

// cacheKey rounds lat/lon to 2 decimal places (~1.1km) and returns a
// string key suitable for use in the cache map. This precision is more
// than sufficient for city-level reverse geocoding: photos taken within
// ~1km of each other will resolve to the same city anyway.
func cacheKey(lat, lon float64) string {
	return fmt.Sprintf("%.2f,%.2f", lat, lon)
}

// cacheEntry holds a cached geocode result. A nil location means the
// coordinates resolved to no known place (ENOTFOUND), which is also cached
// to avoid repeating failed lookups.
type cacheEntry struct {
	location *photo.Location // nil = ENOTFOUND
}

// NominatimGeocoder implements photo.Geocoder using the Nominatim API.
type NominatimGeocoder struct {
	// UserAgent is sent as the HTTP User-Agent header.
	// Nominatim's usage policy requires a descriptive value identifying your
	// application. Example: "photo-manager/0.1 (you@example.com)"
	UserAgent string

	// HTTPClient is used for all outbound requests.
	// Defaults to a client with a 10-second timeout if nil.
	HTTPClient *http.Client

	// mu protects both the rate limiter and the cache.
	mu          sync.Mutex
	lastRequest time.Time
	cache       map[string]cacheEntry
}

// NewNominatimGeocoder returns a new geocoder with sensible defaults.
// userAgent must be a non-empty, descriptive string per Nominatim's policy.
func NewNominatimGeocoder(userAgent string) *NominatimGeocoder {
	return &NominatimGeocoder{
		UserAgent: userAgent,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: make(map[string]cacheEntry),
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

	key := cacheKey(lat, lon)

	// Check the in-process cache before hitting the network.
	g.mu.Lock()
	if entry, ok := g.cache[key]; ok {
		g.mu.Unlock()
		if entry.location == nil {
			return nil, photo.Errorf(photo.ENOTFOUND,
				"geocoder: no place found for (%.6f, %.6f)", lat, lon)
		}
		return entry.location, nil
	}
	g.mu.Unlock()

	// Not cached — enforce rate limit and fetch from Nominatim.
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
	req.Header.Set("Accept-Language", "en")

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return nil, photo.Errorf(photo.EINTERNAL, "geocoder: request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		g.store(key, nil)
		return nil, photo.Errorf(photo.ENOTFOUND,
			"geocoder: no place found for (%.6f, %.6f)", lat, lon)
	}
	if resp.StatusCode != http.StatusOK {
		// Don't cache transient HTTP errors.
		return nil, photo.Errorf(photo.EINTERNAL,
			"geocoder: unexpected status %d for (%.6f, %.6f)", resp.StatusCode, lat, lon)
	}

	var result nominatimResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, photo.Errorf(photo.EINTERNAL, "geocoder: decode response: %v", err)
	}

	if result.Error != "" {
		g.store(key, nil)
		return nil, photo.Errorf(photo.ENOTFOUND, "geocoder: %s", result.Error)
	}

	loc := &photo.Location{
		City:        result.bestCity(),
		Region:      result.Address.State,
		Country:     result.Address.Country,
		CountryCode: strings.ToUpper(result.Address.CountryCode),
	}

	g.store(key, loc)
	return loc, nil
}

// store writes a result to the cache under mu.
func (g *NominatimGeocoder) store(key string, loc *photo.Location) {
	g.mu.Lock()
	g.cache[key] = cacheEntry{location: loc}
	g.mu.Unlock()
}

// rateLimit blocks until it is safe to make the next Nominatim request.
// The lock is released before sleeping so cached lookups are never blocked.
func (g *NominatimGeocoder) rateLimit() {
	g.mu.Lock()
	elapsed := time.Since(g.lastRequest)
	wait := minRequestInterval - elapsed
	g.lastRequest = time.Now()
	g.mu.Unlock()

	if wait > 0 {
		time.Sleep(wait)
	}
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
