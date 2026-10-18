// Package repository reads and writes the upgrade worker's shared state.
//
// The worker owns the Docker socket; this repository only performs atomic
// file operations inside a single state directory. Writes go to a temporary
// file in the same directory and are renamed into place so the worker never
// reads a half-written command.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/service_version/domain"
)

// Repository exposes the worker state files used by the management API.
type Repository interface {
	Status(ctx context.Context) (*domain.StatusSnapshot, error)
	ReleaseCatalog(ctx context.Context) (*domain.ReleaseCatalog, error)
	RequestCheck(ctx context.Context) error
	Enqueue(ctx context.Context, request *domain.UpgradeRequest) error
	EnqueueBatch(ctx context.Context, requests []*domain.UpgradeRequest) error
	ListOperations(ctx context.Context) ([]domain.Operation, error)
	FindOperation(ctx context.Context, operationID string) (*domain.Operation, error)
}

// FileRepository stores state under one directory shared with the worker.
type FileRepository struct {
	stateDir string
}

// NewFileRepository creates a repository rooted at stateDir.
func NewFileRepository(stateDir string) *FileRepository {
	return &FileRepository{stateDir: strings.TrimSpace(stateDir)}
}

func (r *FileRepository) statusPath() string {
	return filepath.Join(r.stateDir, "status.json")
}

func (r *FileRepository) checkPath() string {
	return filepath.Join(r.stateDir, "check-request.json")
}

func (r *FileRepository) releaseCatalogPath() string {
	return filepath.Join(r.stateDir, "releases.json")
}

func (r *FileRepository) queueDir() string {
	return filepath.Join(r.stateDir, "queue")
}

func (r *FileRepository) resultsDir() string {
	return filepath.Join(r.stateDir, "results")
}

func (r *FileRepository) processingDir() string {
	return filepath.Join(r.stateDir, "processing")
}

// Status returns the worker's latest snapshot. A missing file is reported as
// unavailable so the console can show "unknown" rather than a stale guess.
func (r *FileRepository) Status(ctx context.Context) (*domain.StatusSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(r.statusPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.ErrStateUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("read status snapshot: %w", err)
	}
	var snapshot domain.StatusSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil, fmt.Errorf("decode status snapshot: %w", err)
	}
	return &snapshot, nil
}

// ReleaseCatalog returns the worker-produced release list. A missing file
// means the worker has not completed its first refresh yet.
func (r *FileRepository) ReleaseCatalog(
	ctx context.Context,
) (*domain.ReleaseCatalog, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := os.ReadFile(r.releaseCatalogPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.ErrStateUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("read release catalog: %w", err)
	}
	var catalog domain.ReleaseCatalog
	if err := json.Unmarshal(payload, &catalog); err != nil {
		return nil, fmt.Errorf("decode release catalog: %w", err)
	}
	return &catalog, nil
}

// RequestCheck writes a single check marker that the worker consumes.
func (r *FileRepository) RequestCheck(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	request := domain.CheckRequest{RequestedAt: time.Now().UTC()}
	return writeJSONAtomic(r.checkPath(), request)
}

// Enqueue writes one upgrade command into the worker's queue directory.
func (r *FileRepository) Enqueue(
	ctx context.Context,
	request *domain.UpgradeRequest,
) error {
	return r.EnqueueBatch(ctx, []*domain.UpgradeRequest{request})
}

// EnqueueBatch writes one or more upgrade commands while holding a single
// cross-process lock. The queue is intentionally serial: the worker processes
// one operation at a time, so a batch is a set of future steps, not parallel
// work. Idle checks and all writes happen under the same lock to prevent a
// second request from slipping in between individual files.
func (r *FileRepository) EnqueueBatch(
	ctx context.Context,
	requests []*domain.UpgradeRequest,
) error {
	if len(requests) == 0 {
		return errors.New("upgrade request is required")
	}
	seenIDs := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		if request == nil {
			return errors.New("upgrade request is required")
		}
		id := strings.TrimSpace(request.ID)
		if id == "" || strings.ContainsAny(id, `/\`) {
			return errors.New("upgrade request id is invalid")
		}
		if _, exists := seenIDs[id]; exists {
			return errors.New("upgrade request id is duplicated")
		}
		seenIDs[id] = struct{}{}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(r.queueDir(), 0o750); err != nil {
		return fmt.Errorf("create upgrade queue: %w", err)
	}
	if err := r.ensureQueueIdle(); err != nil {
		return err
	}
	lock, err := r.acquireEnqueueLock()
	if err != nil {
		return err
	}
	defer lock.release()
	// Re-check under the repository lock. ensureIdle in the service layer is
	// advisory and can race with another administrator request; this check is
	// the authoritative gate that prevents two active queue entries.
	if err := r.ensureQueueIdle(); err != nil {
		return err
	}
	written := make([]string, 0, len(requests))
	for _, request := range requests {
		path := filepath.Join(r.queueDir(), request.ID+".json")
		if err := writeJSONAtomic(path, request); err != nil {
			for _, writtenPath := range written {
				_ = os.Remove(writtenPath)
			}
			return err
		}
		written = append(written, path)
	}
	return nil
}

type enqueueLock struct {
	file *os.File
	path string
}

func (lock *enqueueLock) release() {
	if lock == nil {
		return
	}
	if lock.file != nil {
		_ = lock.file.Close()
	}
	if lock.path != "" {
		_ = os.Remove(lock.path)
	}
}

const enqueueLockStaleAfter = 15 * time.Minute

// acquireEnqueueLock creates the queue lock with O_EXCL. This is atomic across
// processes sharing the upgrade volume, unlike a process-local mutex. A lock
// left behind by a crashed process is only removed after a conservative
// expiry; the lock file contains both PID and creation time so operators can
// distinguish a live request from a stale artifact.
func (r *FileRepository) acquireEnqueueLock() (*enqueueLock, error) {
	path := filepath.Join(r.queueDir(), ".enqueue.lock")
	now := time.Now().UTC()
	if err := r.removeStaleEnqueueLock(path, now); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil, domain.ErrUpgradeInProgress
	}
	if err != nil {
		return nil, fmt.Errorf("create upgrade queue lock: %w", err)
	}
	payload := fmt.Sprintf("%d %s\n", os.Getpid(), now.Format(time.RFC3339Nano))
	if _, err := io.WriteString(file, payload); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("write upgrade queue lock: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("sync upgrade queue lock: %w", err)
	}
	return &enqueueLock{file: file, path: path}, nil
}

func (r *FileRepository) removeStaleEnqueueLock(
	path string,
	now time.Time,
) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect upgrade queue lock: %w", err)
	}
	if now.Sub(info.ModTime()) <= enqueueLockStaleAfter {
		return domain.ErrUpgradeInProgress
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale upgrade queue lock: %w", err)
	}
	return nil
}

// ensureQueueIdle checks queue files, worker-owned processing files, and
// active results. The worker moves a queue file into processing/ before it
// writes the first result. Checking only queue/ and results/ would therefore
// allow a second request in that claim window.
func (r *FileRepository) ensureQueueIdle() error {
	for _, directory := range []string{r.queueDir(), r.processingDir()} {
		entries, err := os.ReadDir(directory)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("list upgrade state: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() ||
				!strings.HasSuffix(entry.Name(), ".json") ||
				strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			return domain.ErrUpgradeInProgress
		}
	}
	operations, err := r.ListOperations(context.Background())
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if domain.IsActiveOperation(operation.Status) {
			return domain.ErrUpgradeInProgress
		}
	}
	return nil
}

// ListOperations returns every known operation, newest first.
func (r *FileRepository) ListOperations(
	ctx context.Context,
) ([]domain.Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.resultsDir())
	if errors.Is(err, os.ErrNotExist) {
		return []domain.Operation{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list upgrade results: %w", err)
	}
	operations := make([]domain.Operation, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		operation, err := r.readOperation(filepath.Join(r.resultsDir(), entry.Name()))
		if err != nil {
			// A partially written or unreadable file must not break the whole
			// list; operators can still see the remaining history.
			continue
		}
		operations = append(operations, *operation)
	}
	sort.SliceStable(operations, func(left int, right int) bool {
		return operationSortKey(operations[left]) > operationSortKey(operations[right])
	})
	return operations, nil
}

// FindOperation returns one operation by id.
func (r *FileRepository) FindOperation(
	ctx context.Context,
	operationID string,
) (*domain.Operation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" || strings.ContainsAny(operationID, `/\`) {
		return nil, domain.ErrOperationNotFound
	}
	operation, err := r.readOperation(
		filepath.Join(r.resultsDir(), operationID+".json"),
	)
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	return operation, nil
}

func (r *FileRepository) readOperation(path string) (*domain.Operation, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var operation domain.Operation
	if err := json.Unmarshal(payload, &operation); err != nil {
		return nil, fmt.Errorf("decode upgrade result: %w", err)
	}
	return &operation, nil
}

// operationSortKey prefers the most recent timestamp available. Finished
// operations sort by finish time; active operations sort by start time.
func operationSortKey(operation domain.Operation) int64 {
	switch {
	case operation.FinishedAt != nil:
		return operation.FinishedAt.UnixNano()
	case operation.StartedAt != nil:
		return operation.StartedAt.UnixNano()
	default:
		return 0
	}
}

// writeJSONAtomic writes payload to path via a temporary file and rename.
//
// The rename is atomic on the same filesystem, which is why the temporary
// file is created beside the destination instead of in the system temp dir.
func writeJSONAtomic(path string, payload any) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode state file: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".tmp-*.json")
	if err != nil {
		return fmt.Errorf("create state file: %w", err)
	}
	temporaryPath := temporary.Name()
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("write state file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close state file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace state file: %w", err)
	}
	return nil
}
