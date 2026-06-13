package http_test

import (
	"testing"

	photohttp "github.com/mwyvr/photo/http"
)

func TestSafeFilePath(t *testing.T) {
	tests := []struct {
		name    string
		root    string
		rel     string
		wantErr bool
	}{
		{
			name:    "normal path",
			root:    "/library",
			rel:     "2024/06/06gbsfw3xw0gmvqm.jpg",
			wantErr: false,
		},
		{
			name:    "traversal attempt with ..",
			root:    "/library",
			rel:     "../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "absolute path is joined safely by filepath.Join",
			root:    "/library",
			rel:     "/etc/passwd",
			wantErr: false, // filepath.Join("/library", "/etc/passwd") = "/library/etc/passwd"
		},
		{
			name:    "traversal hidden in path",
			root:    "/library",
			rel:     "2024/../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "thumb path",
			root:    "/library",
			rel:     ".photo/thumbs/06gbsfw3xw0gmvqm.jpg",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := photohttp.SafeFilePath(tt.root, tt.rel)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeFilePath(%q, %q) error = %v, wantErr = %v",
					tt.root, tt.rel, err, tt.wantErr)
			}
		})
	}
}
