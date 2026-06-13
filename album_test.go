package photo_test

import (
	"testing"

	"github.com/mwyvr/photo"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"France 2024", "france-2024"},
		{"Hiking/Camping", "hiking-camping"},
		{"Black & White", "black-white"},
		{"Dawson Creek, BC", "dawson-creek-bc"},
		{"Travel", "travel"},
		{"  leading spaces  ", "leading-spaces"},
		{"multiple---dashes", "multiple-dashes"},
		{"UPPERCASE", "uppercase"},
		{"", "album"},
		{"---", "album"},
		{"café", "caf"}, // non-ASCII letters written then stripped by ASCII-only regex
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := photo.Slugify(tt.input)
			if got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
