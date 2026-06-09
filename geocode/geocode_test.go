package geocode_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mwyvr/photo"
	"github.com/mwyvr/photo/geocode"
)

// newTestGeocoder returns a geocoder pointed at the given test server.
func newTestGeocoder(serverURL string) *geocode.NominatimGeocoder {
	g := geocode.NewNominatimGeocoder("photo-test/1.0")
	g.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	// Override base URL via test server — done by setting the unexported field
	// indirectly through a helper in a white-box test, or by using the public API.
	// Since NominatimBaseURL is unexported, we test via the public interface only.
	_ = serverURL
	return g
}

func TestGeocoderCache_HitsOnSecondCall(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"address":{"city":"Paris","country":"France","country_code":"fr"}}`))
	}))
	defer srv.Close()

	g := geocode.NewNominatimGeocoder("photo-test/1.0")
	g.HTTPClient = srv.Client()
	// Use a test helper to override the base URL.
	g.SetBaseURL(srv.URL + "/reverse")

	ctx := context.Background()

	loc1, err := g.ReverseGeocode(ctx, 48.8566, 2.3522)
	if err != nil {
		t.Fatalf("first geocode: %v", err)
	}
	if loc1.City != "Paris" {
		t.Errorf("city = %q, want Paris", loc1.City)
	}

	// Second call with same coords — should hit cache, not the server.
	loc2, err := g.ReverseGeocode(ctx, 48.8566, 2.3522)
	if err != nil {
		t.Fatalf("second geocode: %v", err)
	}
	if loc2.City != "Paris" {
		t.Errorf("cached city = %q, want Paris", loc2.City)
	}

	if callCount != 1 {
		t.Errorf("server called %d times, want 1 (cache should prevent second call)", callCount)
	}
}

func TestGeocoderCache_DifferentCoords_BothFetch(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"address":{"city":"SomeCity","country":"SomeCountry","country_code":"sc"}}`))
	}))
	defer srv.Close()

	g := geocode.NewNominatimGeocoder("photo-test/1.0")
	g.HTTPClient = srv.Client()
	g.SetBaseURL(srv.URL + "/reverse")

	ctx := context.Background()
	g.ReverseGeocode(ctx, 48.8566, 2.3522)   //nolint Paris
	g.ReverseGeocode(ctx, 35.6762, 139.6503) //nolint Tokyo — different coords

	if callCount != 2 {
		t.Errorf("server called %d times, want 2 (different coords should both fetch)", callCount)
	}
}

func TestGeocoderCache_NotFoundCached(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"error":"Unable to geocode"}`))
	}))
	defer srv.Close()

	g := geocode.NewNominatimGeocoder("photo-test/1.0")
	g.HTTPClient = srv.Client()
	g.SetBaseURL(srv.URL + "/reverse")

	ctx := context.Background()

	_, err := g.ReverseGeocode(ctx, 0.0, 0.0)
	if photo.ErrorCode(err) != photo.ENOTFOUND {
		t.Errorf("expected ENOTFOUND, got %q", photo.ErrorCode(err))
	}

	// Second call should use cache, not hit server.
	_, err = g.ReverseGeocode(ctx, 0.0, 0.0)
	if photo.ErrorCode(err) != photo.ENOTFOUND {
		t.Errorf("cached: expected ENOTFOUND, got %q", photo.ErrorCode(err))
	}
	if callCount != 1 {
		t.Errorf("server called %d times, want 1 (ENOTFOUND should be cached)", callCount)
	}
}

func TestGeocoder_MissingUserAgent(t *testing.T) {
	g := &geocode.NominatimGeocoder{}
	_, err := g.ReverseGeocode(context.Background(), 48.8566, 2.3522)
	if photo.ErrorCode(err) != photo.EINTERNAL {
		t.Errorf("expected EINTERNAL for missing UserAgent, got %q", photo.ErrorCode(err))
	}
}
