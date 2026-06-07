// Package mock provides in-memory mock implementations of the photo domain
// interfaces for use in unit tests.
//
// Function fields on each mock let callers inject specific responses per test
// without subclassing. All mocks are safe for concurrent use.
package mock

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/mwyvr/kid"
	"github.com/mwyvr/photo"
)

// --- PhotoService -----------------------------------------------------------

type PhotoService struct {
	mu     sync.Mutex
	photos map[kid.ID]*photo.Photo

	FindPhotoByIDFn func(ctx context.Context, id kid.ID) (*photo.Photo, error)
	FindPhotosFn    func(ctx context.Context, filter photo.PhotoFilter) ([]*photo.Photo, int, error)
	CreatePhotoFn   func(ctx context.Context, p *photo.Photo) error
	UpdatePhotoFn   func(ctx context.Context, id kid.ID, upd photo.PhotoUpdate) (*photo.Photo, error)
	DeletePhotoFn   func(ctx context.Context, id kid.ID) error
}

func NewPhotoService() *PhotoService {
	return &PhotoService{photos: make(map[kid.ID]*photo.Photo)}
}

func (s *PhotoService) FindPhotoByID(ctx context.Context, id kid.ID) (*photo.Photo, error) {
	if s.FindPhotoByIDFn != nil {
		return s.FindPhotoByIDFn(ctx, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.photos[id]
	if !ok {
		return nil, photo.Errorf(photo.ENOTFOUND, "photo not found: %s", id)
	}
	cp := *p
	return &cp, nil
}

func (s *PhotoService) FindPhotos(ctx context.Context, filter photo.PhotoFilter) ([]*photo.Photo, int, error) {
	if s.FindPhotosFn != nil {
		return s.FindPhotosFn(ctx, filter)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*photo.Photo
	for _, p := range s.photos {
		cp := *p
		out = append(out, &cp)
	}
	return out, len(out), nil
}

func (s *PhotoService) CreatePhoto(ctx context.Context, p *photo.Photo) error {
	if s.CreatePhotoFn != nil {
		return s.CreatePhotoFn(ctx, p)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.photos {
		if existing.SHA256 == p.SHA256 {
			return photo.Errorf(photo.ECONFLICT, "duplicate sha256: %s", p.SHA256)
		}
	}
	if p.ID.IsNil() {
		p.ID = kid.New()
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	cp := *p
	s.photos[cp.ID] = &cp
	return nil
}

func (s *PhotoService) UpdatePhoto(ctx context.Context, id kid.ID, upd photo.PhotoUpdate) (*photo.Photo, error) {
	if s.UpdatePhotoFn != nil {
		return s.UpdatePhotoFn(ctx, id, upd)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.photos[id]
	if !ok {
		return nil, photo.Errorf(photo.ENOTFOUND, "photo not found: %s", id)
	}
	if upd.Description != nil {
		p.Description = *upd.Description
	}
	p.UpdatedAt = time.Now()
	cp := *p
	return &cp, nil
}

func (s *PhotoService) DeletePhoto(ctx context.Context, id kid.ID) error {
	if s.DeletePhotoFn != nil {
		return s.DeletePhotoFn(ctx, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.photos[id]; !ok {
		return photo.Errorf(photo.ENOTFOUND, "photo not found: %s", id)
	}
	delete(s.photos, id)
	return nil
}

// Photos returns a snapshot of all stored photos. Used in tests to inspect state.
func (s *PhotoService) Photos() map[kid.ID]*photo.Photo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[kid.ID]*photo.Photo, len(s.photos))
	for k, v := range s.photos {
		cp := *v
		out[k] = &cp
	}
	return out
}

// --- TagService -------------------------------------------------------------

type TagService struct {
	mu        sync.Mutex
	tags      map[string]*photo.Tag     // keyed by normalised name
	photoTags map[kid.ID]map[kid.ID]struct{}

	FindOrCreateTagFn func(ctx context.Context, name string) (*photo.Tag, error)
	AttachTagFn       func(ctx context.Context, photoID, tagID kid.ID) error
	DetachTagFn       func(ctx context.Context, photoID, tagID kid.ID) error
}

func NewTagService() *TagService {
	return &TagService{
		tags:      make(map[string]*photo.Tag),
		photoTags: make(map[kid.ID]map[kid.ID]struct{}),
	}
}

func (s *TagService) FindTagByName(ctx context.Context, name string) (*photo.Tag, error) {
	name = photo.NormalizeTagName(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tags[name]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, photo.Errorf(photo.ENOTFOUND, "tag not found: %q", name)
}

func (s *TagService) FindOrCreateTag(ctx context.Context, name string) (*photo.Tag, error) {
	if s.FindOrCreateTagFn != nil {
		return s.FindOrCreateTagFn(ctx, name)
	}
	name = photo.NormalizeTagName(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tags[name]; ok {
		cp := *t
		return &cp, nil
	}
	t := &photo.Tag{ID: kid.New(), Name: name}
	s.tags[name] = t
	cp := *t
	return &cp, nil
}

func (s *TagService) FindTags(ctx context.Context, filter photo.TagFilter) ([]*photo.Tag, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*photo.Tag
	for _, t := range s.tags {
		cp := *t
		out = append(out, &cp)
	}
	return out, len(out), nil
}

func (s *TagService) AttachTag(ctx context.Context, photoID, tagID kid.ID) error {
	if s.AttachTagFn != nil {
		return s.AttachTagFn(ctx, photoID, tagID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.photoTags[photoID] == nil {
		s.photoTags[photoID] = make(map[kid.ID]struct{})
	}
	s.photoTags[photoID][tagID] = struct{}{}
	return nil
}

func (s *TagService) DetachTag(ctx context.Context, photoID, tagID kid.ID) error {
	if s.DetachTagFn != nil {
		return s.DetachTagFn(ctx, photoID, tagID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.photoTags[photoID][tagID]; !ok {
		return photo.Errorf(photo.ENOTFOUND,
			"tag association not found: photo=%s tag=%s", photoID, tagID)
	}
	delete(s.photoTags[photoID], tagID)
	return nil
}

// --- Importer ---------------------------------------------------------------

type ImporterMock struct {
	ImportFileFn   func(ctx context.Context, srcPath string, opts photo.ImportOptions) photo.ImportResult
	ImportReaderFn func(ctx context.Context, r io.Reader, filename string, opts photo.ImportOptions) photo.ImportResult
}

func (m *ImporterMock) ImportFile(ctx context.Context, srcPath string, opts photo.ImportOptions) photo.ImportResult {
	if m.ImportFileFn != nil {
		return m.ImportFileFn(ctx, srcPath, opts)
	}
	return photo.ImportResult{SourcePath: srcPath}
}

func (m *ImporterMock) ImportReader(ctx context.Context, r io.Reader, filename string, opts photo.ImportOptions) photo.ImportResult {
	if m.ImportReaderFn != nil {
		return m.ImportReaderFn(ctx, r, filename, opts)
	}
	return photo.ImportResult{SourcePath: filename}
}

// --- EXIFExtractor ----------------------------------------------------------

type EXIFExtractor struct {
	ExtractFn       func(ctx context.Context, path string) (*photo.EXIFData, error)
	ExtractReaderFn func(ctx context.Context, r io.Reader, filename string) (*photo.EXIFData, error)
}

func (e *EXIFExtractor) Extract(ctx context.Context, path string) (*photo.EXIFData, error) {
	if e.ExtractFn != nil {
		return e.ExtractFn(ctx, path)
	}
	return &photo.EXIFData{}, nil
}

func (e *EXIFExtractor) ExtractReader(ctx context.Context, r io.Reader, filename string) (*photo.EXIFData, error) {
	if e.ExtractReaderFn != nil {
		return e.ExtractReaderFn(ctx, r, filename)
	}
	return &photo.EXIFData{}, nil
}

// --- Geocoder ---------------------------------------------------------------

type Geocoder struct {
	ReverseGeocodeFn func(ctx context.Context, lat, lon float64) (*photo.Location, error)
}

func (g *Geocoder) ReverseGeocode(ctx context.Context, lat, lon float64) (*photo.Location, error) {
	if g.ReverseGeocodeFn != nil {
		return g.ReverseGeocodeFn(ctx, lat, lon)
	}
	return &photo.Location{
		City:        "Test City",
		Region:      "Test Region",
		Country:     "Test Country",
		CountryCode: "TC",
	}, nil
}
