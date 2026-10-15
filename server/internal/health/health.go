// Package health reports library inventory for an administrator.
// Counts that need a storage walk stay unknown until an explicit scan.
// Responses never include filesystem paths, object keys, or worker errors.
package health

import (
	"context"
	"errors"
	"sync"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/recovery"
)

const (
	maxScanObjects = 200
	pageSize       = 50

	MediaLocal = "local"
	MediaS3    = "s3"

	ScanIdle      = "idle"
	ScanRunning   = "running"
	ScanCompleted = "completed"
	ScanCanceled  = "canceled"
	ScanFailed    = "failed"
)

var (
	ErrUnsupported = errors.New("S3 media checks are not supported")
	ErrBusy        = errors.New("a library scan is already running")
	ErrNoScan      = errors.New("there is no library scan")
	ErrUnavailable = errors.New("original is unavailable")
	ErrBadPost     = errors.New("post id is not valid")
)

type KnownCount struct {
	Known bool `json:"known"`
	Value int  `json:"value"`
}

type KnownBytes struct {
	Known bool  `json:"known"`
	Value int64 `json:"value"`
}

type BackupRef struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

type Summary struct {
	MissingOriginals     KnownCount `json:"missing_originals"`
	MissingThumbnails    KnownCount `json:"missing_thumbnails"`
	FailedThumbnails     KnownCount `json:"failed_thumbnails"`
	ProcessingBacklog    KnownCount `json:"processing_backlog"`
	StorageBytes         KnownBytes `json:"storage_bytes"`
	LatestVerifiedBackup *BackupRef `json:"latest_verified_backup"`
	Media                string     `json:"media"`
	S3Checks             string     `json:"s3_checks,omitempty"`
}

type Scan struct {
	Status    string `json:"status"`
	Checked   int    `json:"checked"`
	Missing   int    `json:"missing"`
	Corrupt   int    `json:"corrupt"`
	Bytes     int64  `json:"bytes"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error,omitempty"`
}

type Page struct {
	PostIDs   []string `json:"post_ids"`
	Truncated bool     `json:"truncated"`
}

type Counts struct {
	MissingThumbnails int
	FailedThumbnails  int
	Backlog           int
}

// Reader supplies cheap database facts. It must not stat object storage.
type Reader interface {
	Counts(ctx context.Context) (Counts, error)
	Originals(ctx context.Context, limit int) ([]Original, error)
	OriginalKey(ctx context.Context, postID string) (string, error)
	MissingThumbnails(ctx context.Context, limit int) (Page, error)
}

type Original struct {
	PostID string
	Key    string
}

// Presence reports one local object. Implementations must not return paths.
type Presence interface {
	Present(ctx context.Context, key string) (size int64, ok bool, err error)
}

// Repairer enqueues one existing derivative job. A second call for the same
// post returns that job instead of inserting another row.
type Repairer interface {
	RepairThumbnail(ctx context.Context, postID, originalKey string) (jobID, status string, err error)
}

type BackupLister interface {
	List(ctx context.Context) (recovery.Listing, error)
}

type Service struct {
	reader  Reader
	present Presence
	repair  Repairer
	backups BackupLister
	media   string

	mu     sync.Mutex
	scan   Scan
	cancel context.CancelFunc
	done   chan struct{}
}

func New(reader Reader, present Presence, repair Repairer, backups BackupLister, media string) *Service {
	if media != MediaS3 {
		media = MediaLocal
	}
	return &Service{
		reader: reader, present: present, repair: repair, backups: backups, media: media,
		scan: Scan{Status: ScanIdle},
	}
}

func (s *Service) Summary(ctx context.Context) (Summary, error) {
	out := Summary{
		Media:            s.media,
		MissingOriginals: KnownCount{},
		StorageBytes:     KnownBytes{},
	}
	if s.media == MediaS3 {
		out.S3Checks = "unsupported"
	}
	if s.reader != nil {
		counts, err := s.reader.Counts(ctx)
		if err != nil {
			return Summary{}, err
		}
		out.MissingThumbnails = KnownCount{Known: true, Value: counts.MissingThumbnails}
		out.FailedThumbnails = KnownCount{Known: true, Value: counts.FailedThumbnails}
		out.ProcessingBacklog = KnownCount{Known: true, Value: counts.Backlog}
	}
	if s.backups != nil {
		listing, err := s.backups.List(ctx)
		if err != nil {
			return Summary{}, err
		}
		for _, backup := range listing.Backups {
			if backup.Status == "available" && backup.ID != "" {
				out.LatestVerifiedBackup = &BackupRef{ID: backup.ID, CreatedAt: backup.CreatedAt}
				break
			}
		}
	}
	s.mu.Lock()
	scan := s.scan
	s.mu.Unlock()
	if scan.Status == ScanCompleted && s.media == MediaLocal {
		out.MissingOriginals = KnownCount{Known: true, Value: scan.Missing}
		out.StorageBytes = KnownBytes{Known: true, Value: scan.Bytes}
	}
	return out, nil
}

func (s *Service) ScanStatus() Scan {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scan
}

// StartScan walks at most maxScanObjects local files. The walk is not bound
// to the HTTP request that started it; CancelScan is the only caller stop.
func (s *Service) StartScan() error {
	if s.media == MediaS3 {
		return ErrUnsupported
	}
	if s.present == nil || s.reader == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	if s.scan.Status == ScanRunning {
		s.mu.Unlock()
		return ErrBusy
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.scan = Scan{Status: ScanRunning}
	s.done = make(chan struct{})
	done := s.done
	s.mu.Unlock()
	go s.runScan(ctx, done)
	return nil
}

func (s *Service) CancelScan() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scan.Status != ScanRunning || s.cancel == nil {
		return ErrNoScan
	}
	s.cancel()
	return nil
}

func (s *Service) WaitScan(ctx context.Context) error {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) runScan(ctx context.Context, done chan struct{}) {
	defer close(done)
	originals, err := s.reader.Originals(ctx, maxScanObjects+1)
	if err != nil {
		s.finish(Scan{Status: ScanFailed, Error: safeScanError(err)})
		return
	}
	truncated := len(originals) > maxScanObjects
	if truncated {
		originals = originals[:maxScanObjects]
	}
	var checked, missing, corrupt int
	var bytes int64
	for _, item := range originals {
		if err := ctx.Err(); err != nil {
			s.finish(Scan{Status: ScanCanceled, Checked: checked, Missing: missing, Corrupt: corrupt, Bytes: bytes, Truncated: truncated})
			return
		}
		size, ok, presentErr := s.present.Present(ctx, item.Key)
		if presentErr != nil {
			if errors.Is(presentErr, context.Canceled) || errors.Is(presentErr, context.DeadlineExceeded) {
				s.finish(Scan{Status: ScanCanceled, Checked: checked, Missing: missing, Corrupt: corrupt, Bytes: bytes, Truncated: truncated})
				return
			}
			s.finish(Scan{Status: ScanFailed, Checked: checked, Error: "media check failed"})
			return
		}
		checked++
		if !ok {
			missing++
			continue
		}
		if size == 0 {
			corrupt++
			continue
		}
		bytes += size
	}
	s.finish(Scan{Status: ScanCompleted, Checked: checked, Missing: missing, Corrupt: corrupt, Bytes: bytes, Truncated: truncated})
}

func (s *Service) finish(scan Scan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scan = scan
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

func (s *Service) Thumbnails(ctx context.Context) (Page, error) {
	if s.reader == nil {
		return Page{PostIDs: []string{}}, nil
	}
	page, err := s.reader.MissingThumbnails(ctx, pageSize+1)
	if err != nil {
		return Page{}, err
	}
	if page.PostIDs == nil {
		page.PostIDs = []string{}
	}
	if len(page.PostIDs) > pageSize {
		page.PostIDs = page.PostIDs[:pageSize]
		page.Truncated = true
	}
	return page, nil
}

func (s *Service) Repair(ctx context.Context, postID string) (string, string, error) {
	if s.media == MediaS3 {
		return "", "", ErrUnsupported
	}
	if !validPostID(postID) {
		return "", "", ErrBadPost
	}
	if s.reader == nil || s.repair == nil {
		return "", "", ErrUnavailable
	}
	key, err := s.reader.OriginalKey(ctx, postID)
	if err != nil {
		return "", "", ErrUnavailable
	}
	if key == "" {
		return "", "", ErrUnavailable
	}
	return s.repair.RepairThumbnail(ctx, postID, key)
}

func validPostID(id string) bool {
	if id == "" || len(id) > 18 {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	return id[0] != '0' || len(id) == 1
}

func safeScanError(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ""
	}
	return "library scan failed"
}
