package photo

import "context"

// Location holds a reverse-geocoded place derived from GPS coordinates.
// It is 1:1 with a Photo and used to populate Photo.LocationName at import.
// The full struct is kept for precision queries in future; the denormalised
// Photo.LocationName field is used for display and search at MVP.
type Location struct {
	City        string
	Region      string
	Country     string
	CountryCode string
}

// DisplayName returns a short human-readable description.
func (l *Location) DisplayName() string {
	if l == nil {
		return ""
	}
	if l.City != "" && l.Country != "" {
		return l.City + ", " + l.Country
	}
	if l.City != "" {
		return l.City
	}
	return l.Country
}

// Geocoder reverse-geocodes GPS coordinates into a Location.
type Geocoder interface {
	// ReverseGeocode converts lat/lon to a Location.
	// Returns ENOTFOUND if the coordinates resolve to no known place.
	// Returns EINTERNAL on transport or parsing errors.
	ReverseGeocode(ctx context.Context, lat, lon float64) (*Location, error)
}
