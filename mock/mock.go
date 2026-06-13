// Package mock provides in-memory mock implementations of the photo domain
// interfaces for use in unit tests.
//
// Function fields on each mock let callers inject specific responses per test
// without subclassing. All mocks are safe for concurrent use.
package mock

import (
	"context"
	"io"
	"strings"
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
	// Default: return a minimal valid JPEG so shouldSkip passes in tests.
	return &photo.EXIFData{FileType: "JPEG", MIMEType: "image/jpeg"}, nil
}

func (e *EXIFExtractor) ExtractReader(ctx context.Context, r io.Reader, filename string) (*photo.EXIFData, error) {
	if e.ExtractReaderFn != nil {
		return e.ExtractReaderFn(ctx, r, filename)
	}
	return &photo.EXIFData{FileType: "JPEG", MIMEType: "image/jpeg"}, nil
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

// --- UserService ------------------------------------------------------------

type UserService struct {
	mu    sync.Mutex
	users map[string]*photo.User // keyed by username
}

func NewUserService() *UserService {
	return &UserService{users: make(map[string]*photo.User)}
}

func (s *UserService) CreateUser(ctx context.Context, u *photo.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[strings.ToLower(u.Username)]; ok {
		return photo.Errorf(photo.ECONFLICT, "username already taken")
	}
	u.ID = kid.New()
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	cp := *u
	s.users[strings.ToLower(u.Username)] = &cp
	return nil
}

func (s *UserService) FindUserByID(ctx context.Context, id kid.ID) (*photo.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.ID == id {
			cp := *u
			return &cp, nil
		}
	}
	return nil, photo.Errorf(photo.ENOTFOUND, "user not found")
}

func (s *UserService) FindUserByUsername(ctx context.Context, username string) (*photo.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[strings.ToLower(username)]
	if !ok {
		return nil, photo.Errorf(photo.ENOTFOUND, "user not found")
	}
	cp := *u
	return &cp, nil
}

func (s *UserService) CountUsers(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.users), nil
}

// --- SessionService ---------------------------------------------------------

type SessionService struct {
	mu       sync.Mutex
	sessions map[string]*photo.Session // keyed by token hash
}

func NewSessionService() *SessionService {
	return &SessionService{sessions: make(map[string]*photo.Session)}
}

func (s *SessionService) CreateSession(ctx context.Context, sess *photo.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.ID = kid.New()
	sess.CreatedAt = time.Now()
	cp := *sess
	s.sessions[sess.TokenHash] = &cp
	return nil
}

func (s *SessionService) FindSessionByTokenHash(ctx context.Context, tokenHash string) (*photo.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[tokenHash]
	if !ok {
		return nil, photo.Errorf(photo.ENOTFOUND, "session not found")
	}
	cp := *sess
	return &cp, nil
}

func (s *SessionService) DeleteSession(ctx context.Context, id kid.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, sess := range s.sessions {
		if sess.ID == id {
			delete(s.sessions, k)
			return nil
		}
	}
	return photo.Errorf(photo.ENOTFOUND, "session not found")
}

func (s *SessionService) DeleteExpiredSessions(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, sess := range s.sessions {
		if sess.IsExpired() {
			delete(s.sessions, k)
		}
	}
	return nil
}

// --- StatusService ----------------------------------------------------------

type StatusService struct{}

func (s *StatusService) LibraryStatus(ctx context.Context, userID *kid.ID) (*photo.LibraryStatus, error) {
	return &photo.LibraryStatus{}, nil
}

// --- AlbumService -----------------------------------------------------------

type AlbumService struct {
	mu     sync.Mutex
	albums map[kid.ID]*photo.Album
}

func NewAlbumService() *AlbumService {
	return &AlbumService{albums: make(map[kid.ID]*photo.Album)}
}

func (s *AlbumService) FindAlbumBySlug(ctx context.Context, slug string) (*photo.Album, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.albums {
		if a.Slug == slug {
			cp := *a
			return &cp, nil
		}
	}
	return nil, photo.Errorf(photo.ENOTFOUND, "album not found: %s", slug)
}

func (s *AlbumService) FindAlbumByID(ctx context.Context, id kid.ID) (*photo.Album, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.albums[id]
	if !ok {
		return nil, photo.Errorf(photo.ENOTFOUND, "album not found")
	}
	cp := *a
	return &cp, nil
}

func (s *AlbumService) FindAlbums(ctx context.Context, filter photo.AlbumFilter) ([]*photo.Album, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*photo.Album
	for _, a := range s.albums {
		if !filter.UserID.IsNil() && a.UserID != filter.UserID {
			continue
		}
		cp := *a
		out = append(out, &cp)
	}
	return out, len(out), nil
}

func (s *AlbumService) CreateAlbum(ctx context.Context, a *photo.Album) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.ID = kid.New()
	if a.Slug == "" {
		a.Slug = photo.Slugify(a.Name)
	}
	a.CreatedAt = time.Now()
	a.UpdatedAt = time.Now()
	cp := *a
	s.albums[cp.ID] = &cp
	return nil
}

func (s *AlbumService) UpdateAlbum(ctx context.Context, id kid.ID, upd photo.AlbumUpdate) (*photo.Album, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.albums[id]
	if !ok {
		return nil, photo.Errorf(photo.ENOTFOUND, "album not found")
	}
	if upd.Name != nil {
		a.Name = *upd.Name
	}
	if upd.Description != nil {
		a.Description = *upd.Description
	}
	cp := *a
	return &cp, nil
}

func (s *AlbumService) DeleteAlbum(ctx context.Context, id kid.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.albums[id]; !ok {
		return photo.Errorf(photo.ENOTFOUND, "album not found")
	}
	delete(s.albums, id)
	return nil
}

func (s *AlbumService) AddPhoto(ctx context.Context, albumID, photoID kid.ID) error {
	return nil
}
func (s *AlbumService) RemovePhoto(ctx context.Context, albumID, photoID kid.ID) error {
	return nil
}
func (s *AlbumService) MovePhoto(ctx context.Context, albumID, photoID, afterPhotoID kid.ID) error {
	return nil
}
func (s *AlbumService) FindAlbumPhotos(ctx context.Context, albumID kid.ID, offset, limit int) ([]*photo.Photo, int, error) {
	return nil, 0, nil
}

// --- BackupService -----------------------------------------------------------

type BackupService struct {
	BackupFn func(ctx context.Context, w io.Writer) error
}

func (s *BackupService) Backup(ctx context.Context, w io.Writer) error {
	if s.BackupFn != nil {
		return s.BackupFn(ctx, w)
	}
	_, err := w.Write([]byte("mock-database-backup"))
	return err
}

// --- InviteService -----------------------------------------------------------

type InviteService struct {
	mu      sync.Mutex
	invites map[string]*photo.Invite // keyed by token
}

func NewInviteService() *InviteService {
	return &InviteService{invites: make(map[string]*photo.Invite)}
}

func (s *InviteService) CreateInvite(ctx context.Context, createdBy kid.ID, ttl time.Duration) (*photo.Invite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	inv := &photo.Invite{
		ID:        kid.New(),
		Token:     kid.New().String(), // reuse kid for a unique-enough token in tests
		CreatedBy: createdBy,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	s.invites[inv.Token] = inv
	cp := *inv
	return &cp, nil
}

func (s *InviteService) FindInviteByToken(ctx context.Context, token string) (*photo.Invite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invites[token]
	if !ok {
		return nil, photo.Errorf(photo.ENOTFOUND, "invite not found")
	}
	cp := *inv
	return &cp, nil
}

func (s *InviteService) MarkInviteUsed(ctx context.Context, token string, usedBy kid.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invites[token]
	if !ok {
		return photo.Errorf(photo.ENOTFOUND, "invite not found")
	}
	if !inv.IsValid() {
		return photo.Errorf(photo.ECONFLICT, "invite already used or expired")
	}
	now := time.Now()
	inv.UsedAt = &now
	inv.UsedBy = &usedBy
	return nil
}

func (s *InviteService) FindInvites(ctx context.Context) ([]*photo.Invite, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*photo.Invite
	for _, inv := range s.invites {
		cp := *inv
		out = append(out, &cp)
	}
	return out, nil
}

func (s *InviteService) DeleteInvite(ctx context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invites[token]
	if !ok || inv.UsedAt != nil {
		return photo.Errorf(photo.ENOTFOUND, "invite not found or already used")
	}
	delete(s.invites, token)
	return nil
}
