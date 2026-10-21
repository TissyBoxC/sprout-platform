// Package service owns safe reads and writes for the shared download volume.
//
// Files are addressed by a slash-separated relative path. Every path is
// validated lexically and then resolved through os.Root, so symlinks and
// traversal cannot escape the configured root. Writes use a sibling temporary
// file followed by rename, which prevents readers from seeing partial files.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/domain"
	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/platform/clock"
)

var (
	versionPattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	segmentPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	channelPattern  = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	platformPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	kindPattern     = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
)

const (
	inventoryReadAttempts = 3
	inventoryDigestLimit  = 256
	immutableIndexPath    = ".release-immutability.json"
)

type immutableIndex struct {
	SchemaVersion int      `json:"schema_version"`
	Paths         []string `json:"paths"`
}

type cachedInventoryDigest struct {
	info   os.FileInfo
	digest string
}

// Service owns the shared download volume.
type Service struct {
	root                *os.Root
	rootDir             string
	staging             *os.Root
	stagingDir          string
	publicBaseURL       string
	immutablePaths      map[string]struct{}
	clock               clock.Clock
	inventoryRetryDelay time.Duration
	inventoryOpen       func(string) (*os.File, error)
	inventoryStat       func(string) (os.FileInfo, error)
	inventoryDigestMu   sync.Mutex
	inventoryDigests    map[string]cachedInventoryDigest
	inventoryDigestKeys []string
	// A single background hasher keeps repeated page loads from queueing
	// readers against a store that already saturates the disk.
	digestWarmUpRunning atomic.Bool
	digestWarmUpContext context.Context
	digestWarmUpCancel  context.CancelFunc
	artifactReadBarrier func([]byte)
}

type releaseDirectoryTarget struct {
	version string
	channel string
}

// ArtifactLookup is a resolved canonical file from the shared download volume.
// It is intentionally independent from the operations module so CI-published
// files can be exposed to release lookup without a reverse dependency.
type ArtifactLookup struct {
	Version     string
	Kind        string
	Platform    string
	DownloadURL string
	SHA256      string
}

// ArtifactVerification is the exact on-disk projection used before an OTA
// release can be published.
type ArtifactVerification struct {
	SizeBytes int64
	SHA256    string
}

// Options contains download-store dependencies.
type Options struct {
	RootDir       string
	StagingDir    string
	PublicBaseURL string
	Clock         clock.Clock
}

// New opens the download store root. The directory must already exist so a
// typo in configuration fails startup instead of creating a new empty tree.
func New(options Options) (*Service, error) {
	rootDir := strings.TrimSpace(options.RootDir)
	if rootDir == "" {
		return nil, errors.New("download store root directory is required")
	}
	absoluteRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve download store root: %w", err)
	}
	info, err := os.Stat(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("stat download store root: %w", err)
	}
	if !info.IsDir() {
		return nil, errors.New("download store root is not a directory")
	}
	root, err := os.OpenRoot(absoluteRoot)
	if err != nil {
		return nil, fmt.Errorf("open download store root: %w", err)
	}
	stagingRoot := root
	stagingDir := strings.TrimSpace(options.StagingDir)
	if stagingDir != "" {
		absoluteStaging, err := filepath.Abs(stagingDir)
		if err != nil {
			_ = root.Close()
			return nil, fmt.Errorf("resolve download staging root: %w", err)
		}
		stagingInfo, err := os.Stat(absoluteStaging)
		if err != nil {
			_ = root.Close()
			return nil, fmt.Errorf("stat download staging root: %w", err)
		}
		if !stagingInfo.IsDir() {
			_ = root.Close()
			return nil, errors.New("download staging root is not a directory")
		}
		stagingRoot, err = os.OpenRoot(absoluteStaging)
		if err != nil {
			_ = root.Close()
			return nil, fmt.Errorf("open download staging root: %w", err)
		}
		stagingDir = absoluteStaging
	}
	timeSource := options.Clock
	if timeSource == nil {
		timeSource = clock.SystemClock{}
	}
	service := &Service{
		root:                root,
		rootDir:             absoluteRoot,
		staging:             stagingRoot,
		stagingDir:          stagingDir,
		publicBaseURL:       strings.TrimRight(strings.TrimSpace(options.PublicBaseURL), "/"),
		immutablePaths:      make(map[string]struct{}),
		clock:               timeSource,
		inventoryRetryDelay: 15 * time.Millisecond,
		inventoryOpen:       root.Open,
		inventoryStat:       root.Stat,
		inventoryDigests:    make(map[string]cachedInventoryDigest),
	}
	service.digestWarmUpContext, service.digestWarmUpCancel = context.WithCancel(
		context.Background(),
	)
	if err := service.loadImmutableIndex(); err != nil {
		service.digestWarmUpCancel()
		_ = root.Close()
		return nil, err
	}
	return service, nil
}

func (s *Service) loadImmutableIndex() error {
	payload, err := s.root.ReadFile(immutableIndexPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read release immutability index: %w", err)
	}
	var index immutableIndex
	if err := json.Unmarshal(payload, &index); err != nil {
		return fmt.Errorf("decode release immutability index: %w", err)
	}
	if index.SchemaVersion != 1 {
		return errors.New("unsupported release immutability index schema")
	}
	for _, relativePath := range index.Paths {
		normalized, err := NormalizeFilePath(relativePath)
		if err != nil {
			return fmt.Errorf("invalid immutable release path: %w", err)
		}
		s.immutablePaths[normalized] = struct{}{}
	}
	return nil
}

func (s *Service) persistImmutableIndex() error {
	paths := make([]string, 0, len(s.immutablePaths))
	for relativePath := range s.immutablePaths {
		paths = append(paths, relativePath)
	}
	sort.Strings(paths)
	return s.writeJSONAtomic(immutableIndexPath, immutableIndex{
		SchemaVersion: 1,
		Paths:         paths,
	})
}

// Close cancels any background digest warm-up and releases the root handle.
func (s *Service) Close() error {
	if s == nil || s.root == nil {
		return nil
	}
	if s.digestWarmUpCancel != nil {
		s.digestWarmUpCancel()
	}
	rootErr := s.root.Close()
	if s.staging != nil && s.staging != s.root {
		if stagingErr := s.staging.Close(); stagingErr != nil && rootErr == nil {
			return stagingErr
		}
	}
	return rootErr
}

// Inventory returns every regular file in the store. Canonical release
// artifacts are annotated with their parsed release metadata.
//
// The listing never hashes inside the request. This store holds 6.1 GB and the
// host throttles disk I/O, so even eight parallel readers needed 52s, far past
// the operator page's 15s timeout. Digests are warmed in the background and a
// file without one is listed with an empty digest until the next refresh.
func (s *Service) Inventory(ctx context.Context) (domain.Inventory, error) {
	if err := ctx.Err(); err != nil {
		return domain.Inventory{}, err
	}
	indexedArtifacts := s.indexedArtifactSet()
	files := make([]domain.File, 0)
	err := s.walkInventory(ctx, ".", func(relativePath string, info os.FileInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !info.Mode().IsRegular() ||
			isTemporaryUploadFile(relativePath) ||
			relativePath == immutableIndexPath {
			return nil
		}
		file, ok, err := s.readInventoryFile(ctx, relativePath, info)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		file.DownloadURL = s.publicURL(relativePath)
		file.Release, _ = ParseArtifactPath(relativePath)
		file.IsIndexed = isArtifactIndexed(relativePath, indexedArtifacts)
		files = append(files, file)
		return nil
	})
	if err != nil {
		return domain.Inventory{}, err
	}
	sort.SliceStable(files, func(left int, right int) bool {
		return files[left].RelativePath < files[right].RelativePath
	})
	s.startInventoryDigestWarmUp()
	return domain.Inventory{
		RootDir:       s.rootDir,
		PublicBaseURL: s.publicBaseURL,
		Files:         files,
		GeneratedAt:   s.clock.Now().UTC(),
	}, nil
}

// startInventoryDigestWarmUp hashes uncached artifacts in the background so a
// later listing can report them. The request context is deliberately not used:
// the work outlives the response that triggered it, and the service context
// ends it at shutdown. A second call while one is running is a no-op, so
// repeated page loads do not pile up readers.
func (s *Service) startInventoryDigestWarmUp() {
	if !s.digestWarmUpRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.digestWarmUpRunning.Store(false)
		ctx := s.digestWarmUpContext
		if ctx == nil {
			return
		}
		s.warmInventoryDigests(ctx, s.inventoryDigestCandidates(ctx))
	}()
}

// inventoryDigestCandidates lists canonical release artifacts whose digest is
// not yet cached for their current identity. Only these are hashed: hashing
// notes and temporary uploads made a cold listing read the entire store for
// values that are never published.
func (s *Service) inventoryDigestCandidates(ctx context.Context) []string {
	candidates := make([]string, 0)
	_ = s.walkInventory(ctx, ".", func(relativePath string, info os.FileInfo) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !info.Mode().IsRegular() ||
			isTemporaryUploadFile(relativePath) ||
			relativePath == immutableIndexPath {
			return nil
		}
		if _, ok := ParseArtifactPath(relativePath); !ok {
			return nil
		}
		if _, ok := s.cachedInventoryDigest(relativePath, info); ok {
			return nil
		}
		candidates = append(candidates, relativePath)
		return nil
	})
	return candidates
}

// DigestForPath returns the SHA-256 of one stored file, reusing the inventory
// cache when it still describes the file on disk.
//
// Publishing needs a digest but listing does not, so the expensive read lives
// here instead of in Inventory. Listing every artifact used to hash 6.1 GB and
// took 96s in production, well past the client's 15s timeout.
func (s *Service) DigestForPath(ctx context.Context, relativePath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	cleaned, err := NormalizeRelativePath(relativePath)
	if err != nil {
		return "", err
	}
	if info, statErr := s.root.Stat(cleaned); statErr == nil {
		if digest, ok := s.cachedInventoryDigest(cleaned, info); ok {
			return digest, nil
		}
	}
	digest, err := s.fileSHA256(cleaned)
	if err != nil {
		return "", err
	}
	if info, statErr := s.root.Stat(cleaned); statErr == nil {
		s.storeInventoryDigest(cleaned, info, digest)
	}
	return digest, nil
}

// warmInventoryDigests hashes the given paths concurrently so a cold inventory
// does not serialise one full read per artifact. Release stores grow past 6 GB,
// and hashing that on one goroutine took 96s and timed out the operator page.
//
// Failures are recorded as an empty digest rather than returned: a single
// unreadable artifact must not fail the whole listing. Callers treat an empty
// digest as "not yet verified".
func (s *Service) warmInventoryDigests(ctx context.Context, paths []string) {
	const workers = 8
	if len(paths) == 0 {
		return
	}
	var (
		waitGroup sync.WaitGroup
		next      int64 = -1
	)
	for worker := 0; worker < workers && worker < len(paths); worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for {
				index := int(atomic.AddInt64(&next, 1))
				if index >= len(paths) || ctx.Err() != nil {
					return
				}
				relativePath := paths[index]
				info, statErr := s.root.Stat(relativePath)
				if statErr != nil {
					continue
				}
				if _, ok := s.cachedInventoryDigest(relativePath, info); ok {
					continue
				}
				digest, hashErr := s.fileSHA256(relativePath)
				if hashErr != nil {
					continue
				}
				s.storeInventoryDigest(relativePath, info, digest)
			}
		}()
	}
	waitGroup.Wait()
}

// readInventoryFile performs a bounded retry for every directory entry. A
// file that disappears during a concurrent upload or deletion is omitted from
// the snapshot. A file that remains present but cannot be read is still
// listed with an empty digest, so one corrupt or permission-blocked entry
// cannot fail the entire download panel.
func (s *Service) readInventoryFile(
	ctx context.Context,
	relativePath string,
	fallbackInfo os.FileInfo,
) (domain.File, bool, error) {
	lastInfo := fallbackInfo
	var lastErr error
	for attempt := 0; attempt < inventoryReadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return domain.File{}, false, err
		}
		file, err := s.inventoryOpen(relativePath)
		if err != nil {
			lastErr = err
			if !s.waitInventoryRetry(ctx) {
				return domain.File{}, false, ctx.Err()
			}
			continue
		}
		info, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			lastErr = statErr
			if !s.waitInventoryRetry(ctx) {
				return domain.File{}, false, ctx.Err()
			}
			continue
		}
		lastInfo = info
		if !info.Mode().IsRegular() {
			_ = file.Close()
			return domain.File{}, false, nil
		}
		// Digests were warmed concurrently before the walk, so this only reads
		// the cache. A miss means the file could not be hashed and is listed
		// with an empty digest instead of stalling the response.
		digest, _ := s.cachedInventoryDigest(relativePath, info)
		if err := file.Close(); err != nil {
			lastErr = err
			if !s.waitInventoryRetry(ctx) {
				return domain.File{}, false, ctx.Err()
			}
			continue
		}
		return inventoryFile(relativePath, info, digest), true, nil
	}
	if errors.Is(lastErr, os.ErrNotExist) {
		return domain.File{}, false, nil
	}
	// Preserve the metadata for an unreadable but still present file. The
	// digest remains empty, so the UI shows the entry without pretending it
	// is safe to publish.
	return inventoryFile(relativePath, lastInfo, ""), true, nil
}

func inventoryFile(relativePath string, info os.FileInfo, digest string) domain.File {
	fileName := path.Base(relativePath)
	return domain.File{
		RelativePath: relativePath,
		FileName:     fileName,
		Name:         fileName,
		Directory:    directoryName(relativePath),
		SizeBytes:    info.Size(),
		ModifiedAt:   info.ModTime().UTC(),
		SHA256:       digest,
	}
}

func (s *Service) cachedInventoryDigest(
	relativePath string,
	info os.FileInfo,
) (string, bool) {
	s.inventoryDigestMu.Lock()
	defer s.inventoryDigestMu.Unlock()
	cached, ok := s.inventoryDigests[relativePath]
	if !ok || cached.digest == "" || cached.info == nil {
		return "", false
	}
	if !os.SameFile(cached.info, info) ||
		cached.info.Size() != info.Size() ||
		!cached.info.ModTime().Equal(info.ModTime()) {
		return "", false
	}
	return cached.digest, true
}

func (s *Service) storeInventoryDigest(
	relativePath string,
	info os.FileInfo,
	digest string,
) {
	s.inventoryDigestMu.Lock()
	defer s.inventoryDigestMu.Unlock()
	_, tracked := s.inventoryDigests[relativePath]
	s.inventoryDigests[relativePath] = cachedInventoryDigest{
		info:   info,
		digest: digest,
	}
	if !tracked {
		s.inventoryDigestKeys = append(s.inventoryDigestKeys, relativePath)
	}
	if len(s.inventoryDigestKeys) <= inventoryDigestLimit {
		return
	}
	oldestPath := s.inventoryDigestKeys[0]
	s.inventoryDigestKeys = s.inventoryDigestKeys[1:]
	delete(s.inventoryDigests, oldestPath)
}

func (s *Service) waitInventoryRetry(ctx context.Context) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	if s.inventoryRetryDelay <= 0 {
		return true
	}
	timer := time.NewTimer(s.inventoryRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Service) walkInventory(
	ctx context.Context,
	directory string,
	visit func(relativePath string, info os.FileInfo) error,
) error {
	entries, err := s.readInventoryDirectory(ctx, directory)
	if err != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if directory == "." {
			return err
		}
		return nil
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		entryPath := path.Join(directory, entry.Name())
		if isTemporaryUploadFile(entryPath) {
			continue
		}
		info, err := s.statInventoryEntry(ctx, entryPath)
		if err != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
			continue
		}
		if info.IsDir() {
			if err := s.walkInventory(ctx, entryPath, visit); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := visit(entryPath, info); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) readInventoryDirectory(
	ctx context.Context,
	directory string,
) ([]os.FileInfo, error) {
	var lastErr error
	for attempt := 0; attempt < inventoryReadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		handle, err := s.inventoryOpen(directory)
		if err == nil {
			entries, readErr := handle.Readdir(-1)
			closeErr := handle.Close()
			if readErr == nil && closeErr == nil {
				return entries, nil
			}
			if readErr == nil {
				readErr = closeErr
			}
			if len(entries) > 0 {
				return entries, nil
			}
			lastErr = readErr
		} else {
			lastErr = err
		}
		if !s.waitInventoryRetry(ctx) {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("read download directory %q: %w", directory, lastErr)
}

func (s *Service) statInventoryEntry(
	ctx context.Context,
	relativePath string,
) (os.FileInfo, error) {
	var lastErr error
	for attempt := 0; attempt < inventoryReadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := s.inventoryStat(relativePath)
		if err == nil {
			return info, nil
		}
		lastErr = err
		if !s.waitInventoryRetry(ctx) {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("stat download entry %q: %w", relativePath, lastErr)
}

func isTemporaryUploadFile(relativePath string) bool {
	name := path.Base(relativePath)
	return strings.HasPrefix(name, ".upload-") &&
		strings.HasSuffix(name, ".tmp")
}

// IndexStatus reports how much of the current release inventory is represented
// by the public manifest and index. A missing index remains a valid first
// deployment state, so it is reported as unavailable rather than failing the
// whole download panel.
func (s *Service) IndexStatus(ctx context.Context) (domain.IndexStatus, error) {
	if err := ctx.Err(); err != nil {
		return domain.IndexStatus{}, err
	}
	inventory, err := s.Inventory(ctx)
	if err != nil {
		return domain.IndexStatus{}, err
	}
	index, err := s.loadIndex()
	if errors.Is(err, os.ErrNotExist) {
		return domain.IndexStatus{
			IsAvailable:      false,
			PendingFileCount: countCanonicalArtifacts(inventory.Files),
			ErrorMessage:     "",
		}, nil
	}
	if err != nil {
		return domain.IndexStatus{
			IsAvailable:      false,
			PendingFileCount: countCanonicalArtifacts(inventory.Files),
			ErrorMessage:     "download index unavailable",
		}, nil
	}
	indexedFileCount := 0
	pending := countCanonicalArtifacts(inventory.Files)
	for _, file := range inventory.Files {
		if file.Release != nil && file.IsIndexed {
			indexedFileCount++
			pending--
		}
	}
	return domain.IndexStatus{
		IsAvailable:      true,
		RefreshedAt:      index.GeneratedAt,
		IndexedFileCount: indexedFileCount,
		PendingFileCount: pending,
	}, nil
}

// FindArtifact resolves the newest indexed file for one release version. The
// lookup accepts the operator-facing kind (client/resource) and maps it to the
// canonical CI directory before scanning manifest files.
func (s *Service) FindArtifact(
	ctx context.Context,
	version string,
	kind string,
	platform string,
	channel string,
) (*ArtifactLookup, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	version = strings.TrimSpace(version)
	kind = strings.TrimSpace(kind)
	platform = strings.TrimSpace(platform)
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = domain.DefaultChannel
	}
	if !versionPattern.MatchString(version) ||
		!kindPattern.MatchString(kind) ||
		!platformPattern.MatchString(platform) ||
		!channelPattern.MatchString(channel) {
		return nil, domain.ErrArtifactNotFound
	}
	expectedKind := artifactDirectoryKind(kind, platform, "")
	expectedPrefix := path.Join(version, channel, platform, expectedKind) + "/"
	inventory, err := s.Inventory(ctx)
	if err != nil {
		return nil, err
	}
	for index := range inventory.Files {
		file := &inventory.Files[index]
		if !file.IsIndexed ||
			file.Release == nil ||
			!strings.HasPrefix(file.RelativePath, expectedPrefix) ||
			file.DownloadURL == "" {
			continue
		}
		// The listing skips digests it has not cached yet, but a device must
		// receive a verifiable artifact, so the matched file is hashed here.
		digest := file.SHA256
		if digest == "" {
			digest, err = s.DigestForPath(ctx, file.RelativePath)
			if err != nil {
				continue
			}
		}
		return &ArtifactLookup{
			Version:     file.Release.Version,
			Kind:        kind,
			Platform:    file.Release.Platform,
			DownloadURL: file.DownloadURL,
			SHA256:      digest,
		}, nil
	}
	return nil, domain.ErrArtifactNotFound
}

// ReadArtifact opens exactly one file below the safe root and returns its
// authoritative size and SHA-256. Callers compare these values to signed
// metadata before publication.
func (s *Service) ReadArtifact(
	ctx context.Context,
	artifactKey string,
) (*ArtifactVerification, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	normalized, err := NormalizeRelativePath(artifactKey)
	if err != nil || normalized != artifactKey {
		return nil, domain.ErrPathInvalid
	}
	file, err := s.root.Open(normalized)
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.ErrFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open release artifact: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat release artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, domain.ErrFileNotFound
	}
	hasher := sha256.New()
	written, digestBytes, err := hashArtifact(context.Background(), file, hasher, s.artifactReadBarrier)
	if err != nil {
		return nil, fmt.Errorf("hash release artifact: %w", err)
	}
	return &ArtifactVerification{
		SizeBytes: written,
		SHA256:    hex.EncodeToString(digestBytes),
	}, nil
}

// UploadFile stores one manually uploaded file.
//
// The caller passes a single reader so the handler can enforce the HTTP body
// limit before the service performs the atomic rename.
func (s *Service) UploadFile(
	ctx context.Context,
	input domain.UploadInput,
	reader io.Reader,
) (domain.File, error) {
	if err := ctx.Err(); err != nil {
		return domain.File{}, err
	}
	if strings.TrimSpace(input.StagingPath) != "" {
		return s.PromoteStagedFile(ctx, domain.PromoteStagedInput{
			StagedPath:   input.StagingPath,
			RelativePath: input.RelativePath,
			SHA256:       input.SHA256,
			SizeBytes:    input.SizeBytes,
		})
	}
	relativePath, err := resolveUploadPath(input)
	if err != nil {
		return domain.File{}, err
	}
	parent := path.Dir(relativePath)
	if parent != "." {
		if err := s.root.MkdirAll(parent, 0o775); err != nil {
			return domain.File{}, fmt.Errorf("create download directory: %w", err)
		}
	}
	temporaryName, temporary, err := s.createTemporary(parent)
	if err != nil {
		return domain.File{}, err
	}
	temporaryPath := path.Join(parent, temporaryName)
	completed := false
	defer func() {
		if !completed {
			_ = s.root.Remove(temporaryPath)
		}
	}()
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, digest), io.LimitReader(reader, domain.MaxUploadBytes+1))
	if err != nil {
		_ = temporary.Close()
		return domain.File{}, fmt.Errorf("write download file: %w", err)
	}
	if written > domain.MaxUploadBytes {
		_ = temporary.Close()
		return domain.File{}, domain.ErrFileTooLarge
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return domain.File{}, fmt.Errorf("sync download file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return domain.File{}, fmt.Errorf("close download file: %w", err)
	}
	if err := s.commitUpload(
		temporaryPath,
		relativePath,
		input.Overwrite,
		digest.Sum(nil),
	); err != nil {
		return domain.File{}, fmt.Errorf("publish download file: %w", err)
	}
	completed = true
	info, err := s.root.Stat(relativePath)
	if err != nil {
		return domain.File{}, fmt.Errorf("stat uploaded download file: %w", err)
	}
	release, _ := ParseArtifactPath(relativePath)
	return domain.File{
		RelativePath: relativePath,
		FileName:     path.Base(relativePath),
		SizeBytes:    info.Size(),
		ModifiedAt:   info.ModTime().UTC(),
		SHA256:       hex.EncodeToString(digest.Sum(nil)),
		Release:      release,
		DownloadURL:  s.publicURL(relativePath),
	}, nil
}

// PromoteStagedFile verifies an SFTP staging upload and commits its bytes to
// the public release volume under the same lock used by manual uploads and OTA
// publication. The staging copy is retained as an audit artifact; only the
// public commit is immutable.
func (s *Service) PromoteStagedFile(
	ctx context.Context,
	input domain.PromoteStagedInput,
) (domain.File, error) {
	if err := ctx.Err(); err != nil {
		return domain.File{}, err
	}
	if s.staging == nil || s.staging == s.root {
		return domain.File{}, errors.New("download staging root is not configured")
	}
	stagedPath, err := NormalizeFilePath(input.StagedPath)
	if err != nil {
		return domain.File{}, err
	}
	relativePath, err := NormalizeFilePath(input.RelativePath)
	if err != nil {
		return domain.File{}, err
	}
	expectedDigest := strings.ToLower(strings.TrimSpace(input.SHA256))
	if input.SizeBytes <= 0 ||
		len(expectedDigest) != sha256.Size*2 {
		return domain.File{}, domain.ErrArtifactMismatch
	}
	if _, err := hex.DecodeString(expectedDigest); err != nil {
		return domain.File{}, domain.ErrArtifactMismatch
	}
	stagedFile, err := s.staging.Open(stagedPath)
	if errors.Is(err, os.ErrNotExist) {
		return domain.File{}, domain.ErrFileNotFound
	}
	if err != nil {
		return domain.File{}, fmt.Errorf("open staged release artifact: %w", err)
	}
	defer stagedFile.Close()
	stagedInfo, err := stagedFile.Stat()
	if err != nil {
		return domain.File{}, fmt.Errorf("stat staged release artifact: %w", err)
	}
	if !stagedInfo.Mode().IsRegular() || stagedInfo.Size() != input.SizeBytes {
		return domain.File{}, domain.ErrArtifactMismatch
	}
	hasher := sha256.New()
	written, err := io.Copy(hasher, io.LimitReader(stagedFile, input.SizeBytes+1))
	if err != nil {
		return domain.File{}, fmt.Errorf("hash staged release artifact: %w", err)
	}
	if written != input.SizeBytes ||
		!strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expectedDigest) {
		return domain.File{}, domain.ErrArtifactMismatch
	}
	if _, err := stagedFile.Seek(0, io.SeekStart); err != nil {
		return domain.File{}, fmt.Errorf("rewind staged release artifact: %w", err)
	}
	parent := path.Dir(relativePath)
	if parent != "." {
		if err := s.root.MkdirAll(parent, 0o775); err != nil {
			return domain.File{}, fmt.Errorf("create download directory: %w", err)
		}
	}
	temporaryName, temporary, err := s.createTemporary(parent)
	if err != nil {
		return domain.File{}, err
	}
	temporaryPath := path.Join(parent, temporaryName)
	completed := false
	defer func() {
		if !completed {
			_ = s.root.Remove(temporaryPath)
		}
	}()
	publicDigest := sha256.New()
	copied, err := io.Copy(
		io.MultiWriter(temporary, publicDigest),
		io.LimitReader(stagedFile, input.SizeBytes+1),
	)
	if err != nil {
		_ = temporary.Close()
		return domain.File{}, fmt.Errorf("copy staged release artifact: %w", err)
	}
	if copied != input.SizeBytes ||
		!strings.EqualFold(hex.EncodeToString(publicDigest.Sum(nil)), expectedDigest) {
		_ = temporary.Close()
		return domain.File{}, domain.ErrArtifactMismatch
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return domain.File{}, fmt.Errorf("sync staged release artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return domain.File{}, fmt.Errorf("close staged release artifact: %w", err)
	}
	if err := s.commitUpload(temporaryPath, relativePath, true, publicDigest.Sum(nil)); err != nil {
		return domain.File{}, fmt.Errorf("promote staged release artifact: %w", err)
	}
	completed = true
	info, err := s.root.Stat(relativePath)
	if err != nil {
		return domain.File{}, fmt.Errorf("stat promoted release artifact: %w", err)
	}
	release, _ := ParseArtifactPath(relativePath)
	return domain.File{
		RelativePath: relativePath,
		FileName:     path.Base(relativePath),
		SizeBytes:    info.Size(),
		ModifiedAt:   info.ModTime().UTC(),
		SHA256:       hex.EncodeToString(publicDigest.Sum(nil)),
		Release:      release,
		DownloadURL:  s.publicURL(relativePath),
	}, nil
}

// DeleteFile removes one file and prunes now-empty parent directories.
func (s *Service) DeleteFile(ctx context.Context, relativePath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	normalized, err := NormalizeFilePath(relativePath)
	if err != nil {
		return err
	}
	s.inventoryDigestMu.Lock()
	defer s.inventoryDigestMu.Unlock()
	if _, immutable := s.immutablePaths[normalized]; immutable {
		return domain.ErrFileImmutable
	}
	info, err := s.root.Stat(normalized)
	if errors.Is(err, os.ErrNotExist) {
		return domain.ErrFileNotFound
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return domain.ErrPathInvalid
	}
	if err := s.root.Remove(normalized); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.ErrFileNotFound
		}
		return fmt.Errorf("delete download file: %w", err)
	}
	delete(s.inventoryDigests, normalized)
	// Removing empty parents keeps the console inventory free of empty
	// directory noise. Root itself is never removed.
	parent := path.Dir(normalized)
	for parent != "." {
		if err := s.root.Remove(parent); err != nil {
			break
		}
		parent = path.Dir(parent)
	}
	return nil
}

// MarkPublishedArtifact protects the exact canonical file signed by an OTA
// manifest from the normal overwrite and delete paths.
// MarkPublishedArtifact records a path as immutable once it has been published.
//
// The existence check runs under the same mutex as commitUpload: reading before
// taking the lock left a window where a concurrent overwrite could complete
// between the read and the mark, so both operations reported success and the
// published artifact's bytes changed after publication.
func (s *Service) MarkPublishedArtifact(relativePath string) error {
	normalized, err := NormalizeFilePath(relativePath)
	if err != nil {
		return err
	}
	s.inventoryDigestMu.Lock()
	defer s.inventoryDigestMu.Unlock()
	// readArtifactLocked is the lock-free body, because ReadArtifact would try to
	// take the same mutex again.
	if _, err := s.readArtifactLocked(context.Background(), normalized); err != nil {
		return err
	}
	if _, exists := s.immutablePaths[normalized]; exists {
		return nil
	}
	s.immutablePaths[normalized] = struct{}{}
	if err := s.persistImmutableIndex(); err != nil {
		delete(s.immutablePaths, normalized)
		return err
	}
	return nil
}

// VerifyAndMarkPublishedArtifact hashes the exact artifact and records its
// immutable status while holding the same store mutex used by upload and
// deletion commits. This closes the verify-then-mark replacement window for
// every writer that goes through this service.
func (s *Service) VerifyAndMarkPublishedArtifact(
	ctx context.Context,
	relativePath string,
	expectedSizeBytes int64,
	expectedSHA256 string,
) error {
	normalized, err := NormalizeFilePath(relativePath)
	if err != nil {
		return err
	}
	if expectedSizeBytes < 0 {
		return domain.ErrArtifactMismatch
	}
	expectedDigest := strings.ToLower(strings.TrimSpace(expectedSHA256))
	if len(expectedDigest) != sha256.Size*2 {
		return domain.ErrArtifactMismatch
	}
	if _, err := hex.DecodeString(expectedDigest); err != nil {
		return domain.ErrArtifactMismatch
	}

	s.inventoryDigestMu.Lock()
	defer s.inventoryDigestMu.Unlock()
	verification, err := s.readArtifactLocked(ctx, normalized)
	if err != nil {
		return err
	}
	if verification.SizeBytes != expectedSizeBytes ||
		!strings.EqualFold(verification.SHA256, expectedDigest) {
		return domain.ErrArtifactMismatch
	}
	if _, exists := s.immutablePaths[normalized]; exists {
		return nil
	}
	s.immutablePaths[normalized] = struct{}{}
	if err := s.persistImmutableIndex(); err != nil {
		delete(s.immutablePaths, normalized)
		return err
	}
	return nil
}

func (s *Service) readArtifactLocked(
	ctx context.Context,
	relativePath string,
) (*ArtifactVerification, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := s.root.Open(relativePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, domain.ErrFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open release artifact: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat release artifact: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, domain.ErrFileNotFound
	}
	written, digestBytes, err := hashArtifact(ctx, file, sha256.New(), s.artifactReadBarrier)
	if err != nil {
		return nil, fmt.Errorf("hash release artifact: %w", err)
	}
	return &ArtifactVerification{
		SizeBytes: written,
		SHA256:    hex.EncodeToString(digestBytes),
	}, nil
}

func hashArtifact(
	ctx context.Context,
	reader io.Reader,
	hasher hash.Hash,
	barrier func([]byte),
) (int64, []byte, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, err
	}
	written, err := io.Copy(hasher, reader)
	if err != nil {
		return 0, nil, err
	}
	digestBytes := hasher.Sum(nil)
	if barrier != nil {
		barrier(append([]byte(nil), digestBytes...))
	}
	return written, digestBytes, nil
}

func (s *Service) isImmutable(relativePath string) bool {
	s.inventoryDigestMu.Lock()
	defer s.inventoryDigestMu.Unlock()
	_, immutable := s.immutablePaths[relativePath]
	return immutable
}

// commitUpload serializes the replacement with immutability publication.
// A pre-write existence check alone is insufficient because an OTA publish
// can mark the path immutable between the check and the final rename.
func (s *Service) commitUpload(
	temporaryPath string,
	relativePath string,
	overwrite bool,
	uploadDigest []byte,
) error {
	s.inventoryDigestMu.Lock()
	defer s.inventoryDigestMu.Unlock()
	if _, immutable := s.immutablePaths[relativePath]; immutable {
		verification, err := s.readArtifactLocked(context.Background(), relativePath)
		if err != nil {
			return err
		}
		if strings.EqualFold(verification.SHA256, hex.EncodeToString(uploadDigest)) {
			return nil
		}
		return domain.ErrFileImmutable
	}
	if !overwrite {
		if _, err := s.root.Stat(relativePath); err == nil {
			return domain.ErrFileAlreadyExists
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := s.root.Rename(temporaryPath, relativePath); err != nil {
		return err
	}
	delete(s.inventoryDigests, relativePath)
	return nil
}

// RefreshIndex regenerates one release manifest and the global index.
func (s *Service) RefreshIndex(
	ctx context.Context,
	input domain.IndexRefreshRequest,
) (domain.IndexRefreshResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.IndexRefreshResult{}, err
	}
	version := strings.TrimSpace(input.Version)
	channel := strings.TrimSpace(input.Channel)
	if channel == "" {
		channel = domain.DefaultChannel
	}
	if !versionPattern.MatchString(version) {
		return domain.IndexRefreshResult{}, domain.ErrReleaseInvalid
	}
	if !channelPattern.MatchString(channel) {
		return domain.IndexRefreshResult{}, domain.ErrReleaseInvalid
	}
	releaseDirectory := path.Join(version, channel)
	if info, err := s.root.Stat(releaseDirectory); err != nil ||
		!info.IsDir() {
		return domain.IndexRefreshResult{}, domain.ErrReleaseNotCreated
	}
	manifest, err := s.collectManifest(releaseDirectory)
	if err != nil {
		return domain.IndexRefreshResult{}, err
	}
	manifest.Version = version
	manifest.Channel = channel
	if len(manifest.Artifacts) == 0 {
		return domain.IndexRefreshResult{}, domain.ErrReleaseInvalid
	}
	if err := s.writeJSONAtomic(path.Join(releaseDirectory, "manifest.json"), manifest); err != nil {
		return domain.IndexRefreshResult{}, err
	}
	index, err := s.loadIndex()
	if err != nil {
		return domain.IndexRefreshResult{}, err
	}
	generatedAt := s.clock.Now().UTC()
	publishedAt := strings.TrimSpace(input.PublishedAt)
	if publishedAt == "" {
		publishedAt = generatedAt.Format(time.RFC3339)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "如此萌屋平台 " + version
	}
	entry := domain.IndexRelease{
		ArtifactCount: len(manifest.Artifacts),
		Channel:       channel,
		ManifestURL:   s.publicURL(path.Join(releaseDirectory, "manifest.json")),
		PublishedAt:   publishedAt,
		Title:         title,
		Version:       version,
	}
	index.GeneratedAt = generatedAt
	index.SchemaVersion = 1
	index.Releases = replaceIndexRelease(index.Releases, entry)
	if err := s.writeJSONAtomic("index.json", index); err != nil {
		return domain.IndexRefreshResult{}, err
	}
	return domain.IndexRefreshResult{
		Version:       version,
		Channel:       channel,
		ArtifactCount: len(manifest.Artifacts),
		ManifestURL:   entry.ManifestURL,
		GeneratedAt:   generatedAt,
	}, nil
}

// RefreshAllIndexes scans every canonical release directory, regenerates each
// manifest, and atomically replaces the global index. This is the operation
// used by the console's "refresh index" action and by release synchronization.
func (s *Service) RefreshAllIndexes(
	ctx context.Context,
) (domain.IndexRefreshSummary, error) {
	if err := ctx.Err(); err != nil {
		return domain.IndexRefreshSummary{}, err
	}
	targets := make([]releaseDirectoryTarget, 0)
	err := walkRoot(s.root, ".", func(relativePath string, info os.FileInfo) error {
		if !info.Mode().IsRegular() || path.Base(relativePath) != "manifest.json" {
			return nil
		}
		segments := strings.Split(relativePath, "/")
		if len(segments) != 3 {
			return nil
		}
		version, channel := segments[0], segments[1]
		if !versionPattern.MatchString(version) ||
			!channelPattern.MatchString(channel) {
			return nil
		}
		targets = append(targets, releaseDirectoryTarget{version: version, channel: channel})
		return nil
	})
	if err != nil {
		return domain.IndexRefreshSummary{}, err
	}
	// A release directory may contain artifacts before its first manifest is
	// generated. Discover those directories too so manual uploads are included
	// without requiring an extra operator step.
	directoryTargets, err := s.releaseDirectories()
	if err != nil {
		return domain.IndexRefreshSummary{}, err
	}
	targets = mergeReleaseTargets(targets, directoryTargets)
	if len(targets) == 0 {
		return domain.IndexRefreshSummary{}, domain.ErrReleaseNotCreated
	}
	sort.SliceStable(targets, func(left int, right int) bool {
		if targets[left].version == targets[right].version {
			return targets[left].channel < targets[right].channel
		}
		return targets[left].version < targets[right].version
	})
	summary := domain.IndexRefreshSummary{
		Versions: make([]string, 0),
		Channels: make([]string, 0),
	}
	for _, target := range targets {
		result, err := s.RefreshIndex(ctx, domain.IndexRefreshRequest{
			Version: target.version,
			Channel: target.channel,
		})
		if errors.Is(err, domain.ErrReleaseInvalid) {
			continue
		}
		if err != nil {
			return domain.IndexRefreshSummary{}, err
		}
		summary.ReleaseCount++
		summary.ArtifactCount += result.ArtifactCount
		summary.Versions = append(summary.Versions, result.Version)
		summary.Channels = append(summary.Channels, result.Channel)
	}
	if summary.ReleaseCount == 0 {
		return domain.IndexRefreshSummary{}, domain.ErrReleaseNotCreated
	}
	status, err := s.IndexStatus(ctx)
	if err != nil {
		return domain.IndexRefreshSummary{}, err
	}
	summary.RefreshedAt = status.RefreshedAt
	summary.IndexedFileCount = status.IndexedFileCount
	summary.PendingFileCount = status.PendingFileCount
	return summary, nil
}

func (s *Service) releaseDirectories() ([]releaseDirectoryTarget, error) {
	targets := make([]releaseDirectoryTarget, 0)
	entries, err := s.readDirectory(".")
	if err != nil {
		return nil, err
	}
	for _, versionEntry := range entries {
		if !versionEntry.IsDir() || !versionPattern.MatchString(versionEntry.Name()) {
			continue
		}
		channelEntries, err := s.readDirectory(versionEntry.Name())
		if err != nil {
			return nil, err
		}
		for _, channelEntry := range channelEntries {
			if !channelEntry.IsDir() || !channelPattern.MatchString(channelEntry.Name()) {
				continue
			}
			targets = append(targets, releaseDirectoryTarget{
				version: versionEntry.Name(),
				channel: channelEntry.Name(),
			})
		}
	}
	return targets, nil
}

func (s *Service) readDirectory(directory string) ([]os.FileInfo, error) {
	handle, err := s.root.Open(directory)
	if err != nil {
		return nil, fmt.Errorf("open download directory: %w", err)
	}
	defer handle.Close()
	entries, err := handle.Readdir(-1)
	if err != nil {
		return nil, fmt.Errorf("read download directory: %w", err)
	}
	return entries, nil
}

func mergeReleaseTargets(
	left []releaseDirectoryTarget,
	right []releaseDirectoryTarget,
) []releaseDirectoryTarget {
	seen := make(map[string]struct{})
	result := make([]releaseDirectoryTarget, 0, len(left)+len(right))
	for _, target := range append(left, right...) {
		key := target.version + "/" + target.channel
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, target)
	}
	return result
}

func (s *Service) collectManifest(releaseDirectory string) (domain.Manifest, error) {
	manifest := domain.Manifest{Artifacts: make([]domain.ManifestArtifact, 0)}
	err := walkRoot(s.root, releaseDirectory, func(relativePath string, info os.FileInfo) error {
		if !info.Mode().IsRegular() || path.Base(relativePath) == "manifest.json" {
			return nil
		}
		release, ok := ParseArtifactPath(relativePath)
		if !ok || release.Version+"/"+release.Channel != releaseDirectory {
			return nil
		}
		digest, err := s.fileSHA256(relativePath)
		if err != nil {
			return err
		}
		relativeToRelease := strings.TrimPrefix(relativePath, releaseDirectory+"/")
		manifest.Artifacts = append(manifest.Artifacts, domain.ManifestArtifact{
			FileName:     path.Base(relativePath),
			Kind:         release.Kind,
			Platform:     release.Platform,
			RelativePath: relativeToRelease,
			SHA256:       digest,
			SizeBytes:    info.Size(),
			URL:          s.publicURL(relativePath),
		})
		return nil
	})
	if err != nil {
		return domain.Manifest{}, err
	}
	sort.SliceStable(manifest.Artifacts, func(left int, right int) bool {
		return manifest.Artifacts[left].RelativePath <
			manifest.Artifacts[right].RelativePath
	})
	return manifest, nil
}

// ParseArtifactPath recognizes the canonical public layout without touching
// the filesystem. The boolean is false for any non-release path.
func ParseArtifactPath(relativePath string) (*domain.FileRelease, bool) {
	normalized, err := NormalizeRelativePath(relativePath)
	if err != nil {
		return nil, false
	}
	segments := strings.Split(normalized, "/")
	if len(segments) != 5 {
		return nil, false
	}
	version := segments[0]
	channel := segments[1]
	platform := segments[2]
	kind := segments[3]
	fileName := segments[4]
	if !versionPattern.MatchString(version) ||
		!channelPattern.MatchString(channel) ||
		!platformPattern.MatchString(platform) ||
		!kindPattern.MatchString(kind) ||
		!segmentPattern.MatchString(fileName) {
		return nil, false
	}
	return &domain.FileRelease{
		Version:  version,
		Channel:  channel,
		Platform: platform,
		Kind:     kind,
	}, true
}

// resolveUploadPath accepts either a complete relative path or the release
// metadata submitted by the console. Structured metadata is authoritative so
// a browser cannot accidentally store an artifact outside the indexed layout.
func resolveUploadPath(input domain.UploadInput) (string, error) {
	version := strings.TrimSpace(input.Version)
	channel := strings.TrimSpace(input.Channel)
	platform := strings.TrimSpace(input.Platform)
	kind := strings.TrimSpace(input.Kind)
	fileName := strings.TrimSpace(input.FileName)
	if version == "" && channel == "" && platform == "" && kind == "" && fileName == "" {
		return NormalizeFilePath(input.RelativePath)
	}
	if channel == "" {
		channel = domain.DefaultChannel
	}
	if !versionPattern.MatchString(version) ||
		!channelPattern.MatchString(channel) ||
		!platformPattern.MatchString(platform) ||
		!kindPattern.MatchString(kind) ||
		!segmentPattern.MatchString(fileName) {
		return "", domain.ErrPathInvalid
	}
	return path.Join(
		version,
		channel,
		platform,
		artifactDirectoryKind(kind, platform, fileName),
		fileName,
	), nil
}

// artifactDirectoryKind keeps operator-facing release kinds aligned with the
// canonical package directories produced by CI. The release record still uses
// client/resource/firmware, while the download URL keeps the transport format.
func artifactDirectoryKind(kind string, platform string, fileName string) string {
	if kind == domain.ArtifactKindClient {
		if platform == "android" &&
			(fileName == "" || strings.HasSuffix(strings.ToLower(fileName), ".apk")) {
			return domain.ArtifactKindAPK
		}
		if platform == "all" &&
			(fileName == "" || strings.HasSuffix(strings.ToLower(fileName), ".tar.gz")) {
			return domain.ArtifactKindAdminWeb
		}
	}
	if kind == domain.ArtifactKindResource &&
		platform == "all" &&
		strings.HasPrefix(fileName, "sprout-contracts-") {
		return domain.ArtifactKindContracts
	}
	return kind
}

// NormalizeFilePath validates a writable relative path.
func NormalizeFilePath(value string) (string, error) {
	normalized, err := NormalizeRelativePath(value)
	if err != nil {
		return "", err
	}
	if path.Base(normalized) == "." || strings.HasSuffix(normalized, "/") {
		return "", domain.ErrPathInvalid
	}
	return normalized, nil
}

// NormalizeRelativePath rejects empty, absolute, traversal, backslash, and
// control-character paths before they reach os.Root.
func NormalizeRelativePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsRune(value, '\\') ||
		strings.HasPrefix(value, "/") || path.IsAbs(value) {
		return "", domain.ErrPathInvalid
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") ||
		strings.ContainsRune(cleaned, '\x00') {
		return "", domain.ErrPathInvalid
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == "" || segment == "." || segment == ".." ||
			!segmentPattern.MatchString(segment) {
			return "", domain.ErrPathInvalid
		}
	}
	return cleaned, nil
}

func (s *Service) fileSHA256(relativePath string) (string, error) {
	file, err := s.root.Open(relativePath)
	if err != nil {
		return "", fmt.Errorf("open download file: %w", err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", fmt.Errorf("hash download file: %w", err)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func (s *Service) createTemporary(parent string) (string, *os.File, error) {
	for attempt := 0; attempt < 16; attempt++ {
		name := fmt.Sprintf(".upload-%d-%d.tmp", os.Getpid(), s.clock.Now().UnixNano()+int64(attempt))
		fullPath := path.Join(parent, name)
		file, err := s.root.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o664)
		if err == nil {
			return name, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, fmt.Errorf("create temporary download file: %w", err)
		}
	}
	return "", nil, errors.New("create temporary download file: exhausted attempts")
}

func (s *Service) loadIndex() (domain.ReleaseIndex, error) {
	payload, err := s.root.ReadFile("index.json")
	if errors.Is(err, os.ErrNotExist) {
		return domain.ReleaseIndex{
			Releases:      make([]domain.IndexRelease, 0),
			SchemaVersion: 1,
		}, nil
	}
	if err != nil {
		return domain.ReleaseIndex{}, fmt.Errorf("read download index: %w", err)
	}
	var index domain.ReleaseIndex
	if err := json.Unmarshal(payload, &index); err != nil {
		// A malformed existing index may be the CI-owned publication record.
		// Refuse to overwrite it with a partial replacement; an operator can
		// repair or remove it deliberately.
		return domain.ReleaseIndex{}, fmt.Errorf("decode download index: %w", err)
	}
	if index.Releases == nil {
		index.Releases = make([]domain.IndexRelease, 0)
	}
	if index.SchemaVersion == 0 {
		index.SchemaVersion = 1
	}
	return index, nil
}

func (s *Service) writeJSONAtomic(relativePath string, payload any) error {
	parent := path.Dir(relativePath)
	if parent != "." {
		if err := s.root.MkdirAll(parent, 0o775); err != nil {
			return fmt.Errorf("create download metadata directory: %w", err)
		}
	}
	temporaryName, temporary, err := s.createTemporary(parent)
	if err != nil {
		return err
	}
	temporaryPath := path.Join(parent, temporaryName)
	completed := false
	defer func() {
		if !completed {
			_ = s.root.Remove(temporaryPath)
		}
	}()
	encoder := json.NewEncoder(temporary)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("encode download metadata: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync download metadata: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close download metadata: %w", err)
	}
	if err := s.root.Rename(temporaryPath, relativePath); err != nil {
		return fmt.Errorf("publish download metadata: %w", err)
	}
	completed = true
	return nil
}

func (s *Service) publicURL(relativePath string) string {
	if s.publicBaseURL == "" {
		return ""
	}
	return s.publicBaseURL + "/" + strings.TrimPrefix(relativePath, "/")
}

func walkRoot(
	root *os.Root,
	directory string,
	visit func(relativePath string, info os.FileInfo) error,
) error {
	directoryHandle, err := root.Open(directory)
	if err != nil {
		return fmt.Errorf("open download directory: %w", err)
	}
	defer directoryHandle.Close()
	entries, err := directoryHandle.Readdir(-1)
	if err != nil {
		return fmt.Errorf("read download directory: %w", err)
	}
	for _, entry := range entries {
		entryPath := path.Join(directory, entry.Name())
		info, err := root.Stat(entryPath)
		if err != nil {
			return fmt.Errorf("stat download entry: %w", err)
		}
		if info.IsDir() {
			if err := walkRoot(root, entryPath, visit); err != nil {
				return err
			}
			continue
		}
		if err := visit(entryPath, info); err != nil {
			return err
		}
	}
	return nil
}

func replaceIndexRelease(
	releases []domain.IndexRelease,
	entry domain.IndexRelease,
) []domain.IndexRelease {
	result := make([]domain.IndexRelease, 0, len(releases)+1)
	for _, release := range releases {
		if release.Version == entry.Version && release.Channel == entry.Channel {
			continue
		}
		result = append(result, release)
	}
	result = append(result, entry)
	sort.SliceStable(result, func(left int, right int) bool {
		if result[left].PublishedAt == result[right].PublishedAt {
			return result[left].Version > result[right].Version
		}
		return result[left].PublishedAt > result[right].PublishedAt
	})
	return result
}

func directoryName(relativePath string) string {
	directory := path.Dir(relativePath)
	if directory == "." {
		return ""
	}
	return directory
}

func isArtifactIndexed(relativePath string, indexed map[string]struct{}) bool {
	if len(indexed) == 0 {
		return false
	}
	_, exists := indexed[relativePath]
	return exists
}

func countCanonicalArtifacts(files []domain.File) int {
	count := 0
	for _, file := range files {
		if file.Release != nil {
			count++
		}
	}
	return count
}

func (s *Service) indexedArtifactSet() map[string]struct{} {
	index, err := s.loadIndex()
	if err != nil {
		return map[string]struct{}{}
	}
	indexed := make(map[string]struct{})
	for _, release := range index.Releases {
		manifestPath := path.Join(release.Version, release.Channel, "manifest.json")
		manifestPayload, readErr := s.root.ReadFile(manifestPath)
		if readErr != nil {
			continue
		}
		var manifest domain.Manifest
		if json.Unmarshal(manifestPayload, &manifest) != nil {
			continue
		}
		for _, artifact := range manifest.Artifacts {
			indexed[path.Join(release.Version, release.Channel, artifact.RelativePath)] =
				struct{}{}
		}
	}
	return indexed
}
