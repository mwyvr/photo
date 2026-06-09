package main

import (
	"reflect"
	"testing"
)

func TestTagsFromPath(t *testing.T) {
	root := "/home/user/Pictures/Publish"

	tests := []struct {
		name     string
		filePath string
		want     []string
	}{
		{
			name:     "file directly under root",
			filePath: "/home/user/Pictures/Publish/IMG_001.jpg",
			want:     nil,
		},
		{
			name:     "single tag directory",
			filePath: "/home/user/Pictures/Publish/travel/IMG_001.jpg",
			want:     []string{"travel"},
		},
		{
			name:     "two tag directories",
			filePath: "/home/user/Pictures/Publish/travel/france/IMG_001.jpg",
			want:     []string{"travel", "france"},
		},
		{
			name:     "year directory skipped",
			filePath: "/home/user/Pictures/Publish/2025/IMG_001.jpg",
			want:     nil,
		},
		{
			name:     "year + tag",
			filePath: "/home/user/Pictures/Publish/2025/travel/IMG_001.jpg",
			want:     []string{"travel"},
		},
		{
			name:     "iso date directory skipped",
			filePath: "/home/user/Pictures/Publish/2025-08-01/IMG_001.jpg",
			want:     nil,
		},
		{
			name:     "year/iso-date both skipped",
			filePath: "/home/user/Pictures/Publish/2025/2025-08-01/IMG_001.jpg",
			want:     nil,
		},
		{
			name:     "year + two tags",
			filePath: "/home/user/Pictures/Publish/2025/travel/france/IMG_001.jpg",
			want:     []string{"travel", "france"},
		},
		{
			name:     "YYYY-MM skipped",
			filePath: "/home/user/Pictures/Publish/2025-08/IMG_001.jpg",
			want:     nil,
		},
		{
			name:     "8-digit date skipped",
			filePath: "/home/user/Pictures/Publish/20250801/IMG_001.jpg",
			want:     nil,
		},
		{
			name:     "tags normalised to lowercase",
			filePath: "/home/user/Pictures/Publish/Travel/France/IMG_001.jpg",
			want:     []string{"travel", "france"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tagsFromPath(root, tt.filePath)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tagsFromPath(%q) = %v, want %v", tt.filePath, got, tt.want)
			}
		})
	}
}

func TestIsDateComponent(t *testing.T) {
	dates := []string{"2024", "2025", "2025-08-01", "2025-08", "08-2025", "20250801"}
	for _, d := range dates {
		if !isDateComponent(d) {
			t.Errorf("isDateComponent(%q) = false, want true", d)
		}
	}

	notDates := []string{"travel", "france", "work", "2024trip", "paris2024", "black-and-white"}
	for _, d := range notDates {
		if isDateComponent(d) {
			t.Errorf("isDateComponent(%q) = true, want false", d)
		}
	}
}
