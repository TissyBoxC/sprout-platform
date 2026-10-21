package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/release_store/domain"
)

type fixedClock struct {
	now time.Time
}

func (clock fixedClock) Now() time.Time {
	return clock.now
}

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	service, err := New(Options{
		RootDir:       root,
		PublicBaseURL: "https://download.example.test",
		Clock: fixedClock{
			now: time.Date(2026, 10, 4, 8, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("create release store service: %v", err)
	}
	t.Cleanup(func() {
		_ = service.Close()
	})
	return service, root
}

func TestInventoryListsNestedFilesAndAnnotatesReleaseArtifacts(t *testing.T) {
	service, root := newTestService(t)
	artifactPath := filepath.Join(
		root,
		"0.12.3",
		"stable",
		"android",
		"apk",
		"sprout-parent-app-v0.12.3.apk",
	)
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		t.Fatalf("create artifact directory: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("apk-bytes"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manual-image.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("write manual file: %v", err)
	}

	inventory, err := service.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory() returned unexpected error: %v", err)
	}
	if len(inventory.Files) != 2 {
		t.Fatalf("expected two files, got %d", len(inventory.Files))
	}
	var releaseFile *domain.File
	for index := range inventory.Files {
		if inventory.Files[index].Release != nil {
			releaseFile = &inventory.Files[index]
		}
	}
	if releaseFile == nil {
		t.Fatal("expected one canonical release artifact")
	}
	if releaseFile.Release.Version != "0.12.3" ||
		releaseFile.Release.Channel != "stable" ||
		releaseFile.Release.Platform != "android" ||
		releaseFile.Release.Kind != "apk" {
		t.Fatalf("unexpected release metadata: %+v", releaseFile.Release)
	}
	if releaseFile.SHA256 != "93e5c2dd0e6b0e5f2c9e6e8c17fd22b7d62075a6e2e81e6b0f8a9c6d5e4f3a2b" &&
		len(releaseFile.SHA256) != 64 {
		t.Fatalf("expected sha256 digest, got %q", releaseFile.SHA256)
	}
	if releaseFile.DownloadURL !=
		"https://download.example.test/0.12.3/stable/android/apk/sprout-parent-app-v0.12.3.apk" {
		t.Fatalf("unexpected download URL: %q", releaseFile.DownloadURL)
	}
}

func TestInventorySkipsTemporaryUploadFiles(t *testing.T) {
	service, root := newTestService(t)
	if err := os.WriteFile(
		filepath.Join(root, ".upload-123-456.tmp"),
		[]byte("partial upload"),
		0o644,
	); err != nil {
		t.Fatalf("write temporary upload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "release-notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatalf("write visible file: %v", err)
	}

	inventory, err := service.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory() returned unexpected error: %v", err)
	}
	if len(inventory.Files) != 1 || inventory.Files[0].RelativePath != "release-notes.txt" {
		t.Fatalf("expected only the published file, got %+v", inventory.Files)
	}
}

func TestInventorySkipsFileRemovedAfterDirectoryListing(t *testing.T) {
	service, root := newTestService(t)
	service.inventoryRetryDelay = 0
	racePath := "0.13.0/stable/any/resource/race.bin"
	stablePath := "0.13.0/stable/any/resource/stable.bin"
	for pathValue, payload := range map[string]string{
		racePath:   "race",
		stablePath: "stable",
	} {
		fullPath := filepath.Join(root, filepath.FromSlash(pathValue))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create artifact directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(payload), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
	}
	open := service.inventoryOpen
	service.inventoryOpen = func(name string) (*os.File, error) {
		if name == racePath {
			return nil, os.ErrNotExist
		}
		return open(name)
	}

	inventory, err := service.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory() returned unexpected error: %v", err)
	}
	if len(inventory.Files) != 1 || inventory.Files[0].RelativePath != stablePath {
		t.Fatalf("expected only the stable artifact, got %+v", inventory.Files)
	}
}

func TestInventoryRetriesTransientEntryFailures(t *testing.T) {
	service, root := newTestService(t)
	service.inventoryRetryDelay = 0
	relativePath := "0.13.0/stable/any/resource/retry.bin"
	fullPath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create retry directory: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("retry"), 0o644); err != nil {
		t.Fatalf("write retry artifact: %v", err)
	}
	open := service.inventoryOpen
	var mu sync.Mutex
	openCalls := 0
	service.inventoryOpen = func(name string) (*os.File, error) {
		if name != relativePath {
			return open(name)
		}
		mu.Lock()
		openCalls++
		attempt := openCalls
		mu.Unlock()
		if attempt < inventoryReadAttempts {
			return nil, os.ErrNotExist
		}
		return open(name)
	}

	inventory, err := service.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory() returned unexpected error: %v", err)
	}
	if len(inventory.Files) != 1 || inventory.Files[0].RelativePath != relativePath {
		t.Fatalf("expected transient entry failure to recover, got %+v", inventory.Files)
	}
}

func TestInventorySupportsConcurrentRequests(t *testing.T) {
	service, root := newTestService(t)
	relativePath := "0.13.0/stable/any/resource/concurrent.bin"
	fullPath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create concurrent directory: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte("concurrent"), 0o644); err != nil {
		t.Fatalf("write concurrent artifact: %v", err)
	}

	const requestCount = 8
	errs := make(chan error, requestCount)
	var group sync.WaitGroup
	for index := 0; index < requestCount; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			inventory, err := service.Inventory(context.Background())
			if err != nil {
				errs <- err
				return
			}
			if len(inventory.Files) != 1 ||
				inventory.Files[0].RelativePath != relativePath ||
				len(inventory.Files[0].SHA256) != 64 {
				errs <- errors.New("concurrent inventory returned an unexpected snapshot")
			}
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent Inventory() failed: %v", err)
	}
}

func TestInventoryDigestCacheIsBounded(t *testing.T) {
	service, root := newTestService(t)
	for index := 0; index < inventoryDigestLimit+32; index++ {
		relativePath := filepath.Join("cache", fmt.Sprintf("file-%03d.bin", index))
		fullPath := filepath.Join(root, relativePath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("create cache directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("cache"), 0o644); err != nil {
			t.Fatalf("write cache file: %v", err)
		}
	}

	if _, err := service.Inventory(context.Background()); err != nil {
		t.Fatalf("Inventory() returned unexpected error: %v", err)
	}
	if len(service.inventoryDigests) > inventoryDigestLimit {
		t.Fatalf(
			"expected at most %d cached digests, got %d",
			inventoryDigestLimit,
			len(service.inventoryDigests),
		)
	}
	if len(service.inventoryDigestKeys) > inventoryDigestLimit {
		t.Fatalf(
			"expected at most %d cache keys, got %d",
			inventoryDigestLimit,
			len(service.inventoryDigestKeys),
		)
	}
}

func TestUploadFileWritesAtomicallyAndRejectsDuplicate(t *testing.T) {
	service, root := newTestService(t)
	uploaded, err := service.UploadFile(
		context.Background(),
		domain.UploadInput{
			RelativePath: "0.13.0/stable/any/notes/readme.txt",
		},
		strings.NewReader("release notes"),
	)
	if err != nil {
		t.Fatalf("UploadFile() returned unexpected error: %v", err)
	}
	if uploaded.RelativePath != "0.13.0/stable/any/notes/readme.txt" {
		t.Fatalf("unexpected relative path: %q", uploaded.RelativePath)
	}
	payload, err := os.ReadFile(filepath.Join(
		root,
		"0.13.0",
		"stable",
		"any",
		"notes",
		"readme.txt",
	))
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if string(payload) != "release notes" {
		t.Fatalf("unexpected uploaded payload: %q", payload)
	}
	_, err = service.UploadFile(
		context.Background(),
		domain.UploadInput{
			RelativePath: "0.13.0/stable/any/notes/readme.txt",
		},
		strings.NewReader("duplicate"),
	)
	if !errors.Is(err, domain.ErrFileAlreadyExists) {
		t.Fatalf("expected duplicate upload rejection, got %v", err)
	}
}

func TestUploadFileRejectsPathTraversal(t *testing.T) {
	service, _ := newTestService(t)
	for _, relativePath := range []string{
		"../outside.txt",
		"/absolute.txt",
		"release/../../outside.txt",
		`release\..\outside.txt`,
		"",
	} {
		_, err := service.UploadFile(
			context.Background(),
			domain.UploadInput{RelativePath: relativePath},
			strings.NewReader("blocked"),
		)
		if !errors.Is(err, domain.ErrPathInvalid) {
			t.Fatalf("expected %q to be rejected, got %v", relativePath, err)
		}
	}
}

func TestUploadFileRejectsOversizedStream(t *testing.T) {
	service, _ := newTestService(t)
	reader := bytes.NewReader(make([]byte, int(domain.MaxUploadBytes+1)))
	_, err := service.UploadFile(
		context.Background(),
		domain.UploadInput{RelativePath: "large.bin"},
		reader,
	)
	if !errors.Is(err, domain.ErrFileTooLarge) {
		t.Fatalf("expected oversized upload rejection, got %v", err)
	}
}

func TestDeleteFileRemovesFileAndEmptyParents(t *testing.T) {
	service, root := newTestService(t)
	relativePath := "0.13.0/stable/any/notes/readme.txt"
	if _, err := service.UploadFile(
		context.Background(),
		domain.UploadInput{RelativePath: relativePath},
		strings.NewReader("release notes"),
	); err != nil {
		t.Fatalf("seed upload: %v", err)
	}
	if err := service.DeleteFile(context.Background(), relativePath); err != nil {
		t.Fatalf("DeleteFile() returned unexpected error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "0.13.0")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected empty release directories to be removed, got %v", err)
	}
	if err := service.DeleteFile(context.Background(), relativePath); !errors.Is(
		err,
		domain.ErrFileNotFound,
	) {
		t.Fatalf("expected missing file error, got %v", err)
	}
}

func TestPublishedArtifactRemainsImmutableAfterServiceRestart(t *testing.T) {
	service, root := newTestService(t)
	relativePath := "0.13.0/stable/esp32_s3/firmware/firmware.bin"
	if _, err := service.UploadFile(
		context.Background(),
		domain.UploadInput{RelativePath: relativePath},
		strings.NewReader("signed firmware"),
	); err != nil {
		t.Fatalf("seed upload: %v", err)
	}
	if err := service.MarkPublishedArtifact(relativePath); err != nil {
		t.Fatalf("mark published artifact: %v", err)
	}
	if _, err := service.UploadFile(
		context.Background(),
		domain.UploadInput{RelativePath: relativePath, Overwrite: true},
		strings.NewReader("replacement firmware"),
	); !errors.Is(err, domain.ErrFileImmutable) {
		t.Fatalf("overwrite error = %v, want immutable", err)
	}
	if err := service.DeleteFile(context.Background(), relativePath); !errors.Is(
		err,
		domain.ErrFileImmutable,
	) {
		t.Fatalf("delete error = %v, want immutable", err)
	}
	if err := service.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}

	restarted, err := New(Options{
		RootDir:       root,
		PublicBaseURL: "https://download.example.test",
	})
	if err != nil {
		t.Fatalf("restart release store: %v", err)
	}
	t.Cleanup(func() {
		_ = restarted.Close()
	})
	if _, err := restarted.UploadFile(
		context.Background(),
		domain.UploadInput{RelativePath: relativePath, Overwrite: true},
		strings.NewReader("replacement after restart"),
	); !errors.Is(err, domain.ErrFileImmutable) {
		t.Fatalf("post-restart overwrite error = %v, want immutable", err)
	}
	inventory, err := restarted.Inventory(context.Background())
	if err != nil {
		t.Fatalf("inventory after restart: %v", err)
	}
	for _, file := range inventory.Files {
		if file.RelativePath == immutableIndexPath {
			t.Fatal("internal immutability index leaked into inventory")
		}
	}
}

func TestPublishedArtifactCannotBeOverwrittenByConcurrentUpload(t *testing.T) {
	service, _ := newTestService(t)
	relativePath := "0.13.1/stable/esp32_s3/firmware/firmware.bin"
	if _, err := service.UploadFile(
		context.Background(),
		domain.UploadInput{RelativePath: relativePath},
		strings.NewReader("signed firmware"),
	); err != nil {
		t.Fatalf("seed upload: %v", err)
	}

	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	errs := make(chan error, 2)
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		<-start
		errs <- service.MarkPublishedArtifact(relativePath)
	}()
	go func() {
		defer waitGroup.Done()
		<-start
		_, err := service.UploadFile(
			context.Background(),
			domain.UploadInput{
				RelativePath: relativePath,
				Overwrite:    true,
			},
			strings.NewReader("replacement firmware"),
		)
		errs <- err
	}()
	close(start)
	waitGroup.Wait()
	close(errs)

	errorsByOperation := make([]error, 0, 2)
	for err := range errs {
		errorsByOperation = append(errorsByOperation, err)
	}
	if len(errorsByOperation) != 2 {
		t.Fatalf("unexpected upload/publish result count: %d", len(errorsByOperation))
	}
	verification, err := service.ReadArtifact(context.Background(), relativePath)
	if err != nil {
		t.Fatalf("read artifact after concurrent operations: %v", err)
	}
	if errors.Is(errorsByOperation[1], domain.ErrFileImmutable) ||
		errors.Is(errorsByOperation[0], domain.ErrFileImmutable) {
		if verification.SHA256 != sha256String("signed firmware") {
			t.Fatalf("immutable artifact bytes changed: %s", verification.SHA256)
		}
		if _, err := service.UploadFile(
			context.Background(),
			domain.UploadInput{RelativePath: relativePath, Overwrite: true},
			strings.NewReader("replacement after immutable"),
		); !errors.Is(err, domain.ErrFileImmutable) {
			t.Fatalf("post-publish overwrite error = %v, want immutable", err)
		}
		return
	}
	t.Fatalf("neither concurrent operation observed immutability: %+v", errorsByOperation)
}

func TestAtomicPublishChecksBytesBeforeReplacementCanCommit(t *testing.T) {
	service, _ := newTestService(t)
	relativePath := "0.13.2/stable/esp32_s3/firmware/firmware.bin"
	if _, err := service.UploadFile(
		context.Background(),
		domain.UploadInput{RelativePath: relativePath},
		strings.NewReader("signed firmware"),
	); err != nil {
		t.Fatalf("seed upload: %v", err)
	}
	replacementStarted := make(chan struct{})
	replacementFinished := make(chan error, 1)
	var startOnce sync.Once
	service.artifactReadBarrier = func(_ []byte) {
		startOnce.Do(func() {
			go func() {
				close(replacementStarted)
				_, err := service.UploadFile(
					context.Background(),
					domain.UploadInput{RelativePath: relativePath, Overwrite: true},
					strings.NewReader("replacement firmware"),
				)
				replacementFinished <- err
			}()
		})
	}
	if err := service.VerifyAndMarkPublishedArtifact(
		context.Background(),
		relativePath,
		int64(len("signed firmware")),
		sha256String("signed firmware"),
	); err != nil {
		t.Fatalf("atomic verify and mark: %v", err)
	}
	select {
	case <-replacementStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("replacement upload did not reach the atomic commit")
	}
	select {
	case err := <-replacementFinished:
		if !errors.Is(err, domain.ErrFileImmutable) {
			t.Fatalf("replacement error = %v, want immutable", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("replacement upload did not return after atomic publish")
	}
	verification, err := service.ReadArtifact(context.Background(), relativePath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if verification.SHA256 != sha256String("signed firmware") {
		t.Fatalf("artifact bytes changed: %s", verification.SHA256)
	}
}

func TestPromoteStagedFileVerifiesBytesAndPublishesAtomically(t *testing.T) {
	root := t.TempDir()
	stagingRoot := t.TempDir()
	service, err := New(Options{
		RootDir:       root,
		StagingDir:    stagingRoot,
		PublicBaseURL: "https://download.example.test",
	})
	if err != nil {
		t.Fatalf("create release store service: %v", err)
	}
	t.Cleanup(func() {
		_ = service.Close()
	})
	stagedPath := "android/apk/sprout-parent-app.apk"
	fullStagedPath := filepath.Join(stagingRoot, filepath.FromSlash(stagedPath))
	if err := os.MkdirAll(filepath.Dir(fullStagedPath), 0o775); err != nil {
		t.Fatalf("create staging directory: %v", err)
	}
	payload := []byte("verified staged apk")
	if err := os.WriteFile(fullStagedPath, payload, 0o664); err != nil {
		t.Fatalf("write staged artifact: %v", err)
	}
	promoted, err := service.PromoteStagedFile(
		context.Background(),
		domain.PromoteStagedInput{
			StagedPath:   stagedPath,
			RelativePath: "0.14.0/stable/android/apk/sprout-parent-app.apk",
			SHA256:       sha256String(string(payload)),
			SizeBytes:    int64(len(payload)),
		},
	)
	if err != nil {
		t.Fatalf("promote staged artifact: %v", err)
	}
	if promoted.SHA256 != sha256String(string(payload)) {
		t.Fatalf("promoted digest = %s", promoted.SHA256)
	}
	verification, err := service.ReadArtifact(
		context.Background(),
		promoted.RelativePath,
	)
	if err != nil {
		t.Fatalf("read promoted artifact: %v", err)
	}
	if verification.SHA256 != sha256String(string(payload)) {
		t.Fatalf("public artifact digest = %s", verification.SHA256)
	}
	if _, err := service.PromoteStagedFile(
		context.Background(),
		domain.PromoteStagedInput{
			StagedPath:   stagedPath,
			RelativePath: promoted.RelativePath,
			SHA256:       sha256String("different bytes"),
			SizeBytes:    int64(len("different bytes")),
		},
	); !errors.Is(err, domain.ErrArtifactMismatch) {
		t.Fatalf("bad staged digest error = %v, want artifact mismatch", err)
	}
}

func sha256String(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestRefreshIndexMatchesPublicReleaseContract(t *testing.T) {
	service, root := newTestService(t)
	relativePath := "0.12.3/stable/android/apk/sprout-parent-app-v0.12.3.apk"
	if _, err := service.UploadFile(
		context.Background(),
		domain.UploadInput{RelativePath: relativePath},
		strings.NewReader("apk"),
	); err != nil {
		t.Fatalf("seed upload: %v", err)
	}
	result, err := service.RefreshIndex(context.Background(), domain.IndexRefreshRequest{
		Version:     "0.12.3",
		Channel:     "stable",
		Title:       "如此萌屋平台 0.12.3",
		PublishedAt: "2026-10-04T08:00:00Z",
	})
	if err != nil {
		t.Fatalf("RefreshIndex() returned unexpected error: %v", err)
	}
	if result.ArtifactCount != 1 {
		t.Fatalf("expected one artifact, got %d", result.ArtifactCount)
	}
	manifestPayload, err := os.ReadFile(filepath.Join(
		root,
		"0.12.3",
		"stable",
		"manifest.json",
	))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest domain.Manifest
	if err := json.Unmarshal(manifestPayload, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Version != "0.12.3" ||
		manifest.Channel != "stable" ||
		len(manifest.Artifacts) != 1 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	artifact := manifest.Artifacts[0]
	if artifact.FileName != "sprout-parent-app-v0.12.3.apk" ||
		artifact.Platform != "android" ||
		artifact.Kind != "apk" ||
		artifact.RelativePath != "android/apk/sprout-parent-app-v0.12.3.apk" ||
		artifact.SizeBytes != 3 ||
		artifact.URL != "https://download.example.test/"+relativePath {
		t.Fatalf("unexpected manifest artifact: %+v", artifact)
	}
	indexPayload, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var index domain.ReleaseIndex
	if err := json.Unmarshal(indexPayload, &index); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if index.SchemaVersion != 1 || len(index.Releases) != 1 {
		t.Fatalf("unexpected index: %+v", index)
	}
	entry := index.Releases[0]
	if entry.Version != "0.12.3" ||
		entry.Channel != "stable" ||
		entry.ArtifactCount != 1 ||
		entry.ManifestURL != "https://download.example.test/0.12.3/stable/manifest.json" {
		t.Fatalf("unexpected index entry: %+v", entry)
	}
}

func TestRefreshIndexKeepsSameVersionChannelsSeparate(t *testing.T) {
	service, root := newTestService(t)
	artifacts := map[string]string{
		"0.12.3/stable/android/apk/stable.apk": "stable",
		"0.12.3/beta/android/apk/beta.apk":     "beta",
	}
	for relativePath, body := range artifacts {
		if _, err := service.UploadFile(
			context.Background(),
			domain.UploadInput{RelativePath: relativePath},
			strings.NewReader(body),
		); err != nil {
			t.Fatalf("seed %s: %v", relativePath, err)
		}
	}
	for _, channel := range []string{"stable", "beta"} {
		if _, err := service.RefreshIndex(
			context.Background(),
			domain.IndexRefreshRequest{Version: "0.12.3", Channel: channel},
		); err != nil {
			t.Fatalf("refresh %s: %v", channel, err)
		}
	}
	payload, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var index domain.ReleaseIndex
	if err := json.Unmarshal(payload, &index); err != nil {
		t.Fatalf("decode index: %v", err)
	}
	if len(index.Releases) != 2 {
		t.Fatalf("same-version channels collapsed: %+v", index.Releases)
	}
	channels := map[string]bool{}
	for _, release := range index.Releases {
		channels[release.Channel] = true
	}
	if !channels["stable"] || !channels["beta"] {
		t.Fatalf("expected stable and beta index entries, got %+v", channels)
	}
}

func TestUploadFileBuildsCanonicalReleasePathFromMetadata(t *testing.T) {
	service, root := newTestService(t)
	uploaded, err := service.UploadFile(
		context.Background(),
		domain.UploadInput{
			Version:  "0.12.4",
			Channel:  "stable",
			Platform: "android",
			Kind:     "client",
			FileName: "sprout-parent-app-v0.12.4.apk",
		},
		strings.NewReader("apk"),
	)
	if err != nil {
		t.Fatalf("UploadFile() returned unexpected error: %v", err)
	}
	expected := "0.12.4/stable/android/apk/sprout-parent-app-v0.12.4.apk"
	if uploaded.RelativePath != expected {
		t.Fatalf("expected canonical path %q, got %q", expected, uploaded.RelativePath)
	}
	if uploaded.Release == nil ||
		uploaded.Release.Version != "0.12.4" ||
		uploaded.Release.Channel != "stable" ||
		uploaded.Release.Platform != "android" ||
		uploaded.Release.Kind != "apk" ||
		uploaded.Release.RegistrationKind() != "client" {
		t.Fatalf("unexpected release metadata: %+v", uploaded.Release)
	}
	if _, err := os.Stat(filepath.Join(root, expected)); err != nil {
		t.Fatalf("expected canonical file to exist: %v", err)
	}
}

func TestReadArtifactReturnsExactSizeDigestAndRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	service, err := New(Options{
		RootDir:       root,
		PublicBaseURL: "https://download.example.test",
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	defer service.Close()

	relativePath := "0.24.1/stable/esp32_s3/firmware/firmware.bin"
	fullPath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("create release directory: %v", err)
	}
	payload := []byte("firmware-bytes")
	if err := os.WriteFile(fullPath, payload, 0o644); err != nil {
		t.Fatalf("write firmware: %v", err)
	}

	verified, err := service.ReadArtifact(context.Background(), relativePath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if verified.SizeBytes != int64(len(payload)) ||
		verified.SHA256 != fmt.Sprintf("%x", sha256.Sum256(payload)) {
		t.Fatalf("unexpected artifact verification: %+v", verified)
	}

	for _, invalid := range []string{
		"../firmware.bin",
		"/etc/passwd",
		"0.24.1/stable/../firmware.bin",
	} {
		if _, err := service.ReadArtifact(context.Background(), invalid); !errors.Is(
			err,
			domain.ErrPathInvalid,
		) {
			t.Fatalf("path %q error = %v, want invalid path", invalid, err)
		}
	}
	if _, err := service.ReadArtifact(
		context.Background(),
		"0.24.1/stable/esp32_s3/firmware/missing.bin",
	); !errors.Is(err, domain.ErrFileNotFound) {
		t.Fatalf("missing file error = %v, want file not found", err)
	}
}

func TestFindArtifactResolvesIndexedCIFileByRegistrationKind(t *testing.T) {
	service, root := newTestService(t)
	releaseDirectory := filepath.Join(root, "0.12.4", "stable", "android", "apk")
	if err := os.MkdirAll(releaseDirectory, 0o755); err != nil {
		t.Fatalf("create release directory: %v", err)
	}
	artifactPath := filepath.Join(releaseDirectory, "sprout-parent-app-v0.12.4.apk")
	if err := os.WriteFile(artifactPath, []byte("apk"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if _, err := service.RefreshIndex(
		context.Background(),
		domain.IndexRefreshRequest{Version: "0.12.4", Channel: "stable"},
	); err != nil {
		t.Fatalf("refresh release index: %v", err)
	}

	artifact, err := service.FindArtifact(
		context.Background(),
		"0.12.4",
		"client",
		"android",
		"stable",
	)
	if err != nil {
		t.Fatalf("FindArtifact() returned unexpected error: %v", err)
	}
	if artifact.Kind != "client" ||
		artifact.DownloadURL !=
			"https://download.example.test/0.12.4/stable/android/apk/sprout-parent-app-v0.12.4.apk" {
		t.Fatalf("unexpected artifact: %+v", artifact)
	}
}

func TestRefreshIndexRejectsInvalidReleaseDirectory(t *testing.T) {
	service, _ := newTestService(t)
	_, err := service.RefreshIndex(context.Background(), domain.IndexRefreshRequest{
		Version: "0.12.3",
		Channel: "stable",
	})
	if !errors.Is(err, domain.ErrReleaseNotCreated) {
		t.Fatalf("expected missing release directory error, got %v", err)
	}
}

func TestParseArtifactPathRejectsNonCanonicalPaths(t *testing.T) {
	for _, value := range []string{
		"0.12.3/stable/android/apk",
		"0.12.3/stable/android/apk/extra/file.apk",
		"v0.12.3/stable/android/apk/file.apk",
		"0.12.3/stable/android/apk/../file.apk",
		"0.12.3/stable/android/apk/file apk",
	} {
		if release, ok := ParseArtifactPath(value); ok {
			t.Fatalf("expected %q to be rejected, got %+v", value, release)
		}
	}
}

// TestInventoryHashesDigestsConcurrently pins the fix for a production timeout:
// hashing 6.1 GB of artifacts on one goroutine took 96s and the operator page
// gave up after 15s. Concurrent warming must produce the same digests.
func TestInventoryHashesDigestsConcurrently(t *testing.T) {
	service, root := newTestService(t)
	directory := filepath.Join(root, "0.40.0", "stable", "any", "docs")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	const fileCount = 24
	expected := make(map[string]string, fileCount)
	for index := 0; index < fileCount; index++ {
		name := fmt.Sprintf("note-%02d.txt", index)
		payload := bytes.Repeat([]byte{byte('a' + index%26)}, 4096)
		if err := os.WriteFile(filepath.Join(directory, name), payload, 0o644); err != nil {
			t.Fatalf("write file %s: %v", name, err)
		}
		sum := sha256.Sum256(payload)
		expected[path.Join("0.40.0", "stable", "any", "docs", name)] = hex.EncodeToString(sum[:])
	}

	inventory, err := service.Inventory(context.Background())
	if err != nil {
		t.Fatalf("Inventory() returned unexpected error: %v", err)
	}
	if len(inventory.Files) != fileCount {
		t.Fatalf("expected %d files, got %d", fileCount, len(inventory.Files))
	}
	for _, file := range inventory.Files {
		want, ok := expected[file.RelativePath]
		if !ok {
			t.Fatalf("unexpected file in inventory: %s", file.RelativePath)
		}
		if file.SHA256 != want {
			t.Fatalf("digest mismatch for %s: got %q, want %q", file.RelativePath, file.SHA256, want)
		}
	}
}

// TestInventoryReusesCachedDigestsAcrossCalls guarantees a warm store costs no
// file reads: the second listing must not re-hash anything.
func TestInventoryReusesCachedDigestsAcrossCalls(t *testing.T) {
	service, root := newTestService(t)
	directory := filepath.Join(root, "0.41.0", "stable", "any", "docs")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	target := filepath.Join(directory, "note.txt")
	if err := os.WriteFile(target, []byte("payload"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	first, err := service.Inventory(context.Background())
	if err != nil {
		t.Fatalf("first Inventory() returned unexpected error: %v", err)
	}
	// A second listing must reuse the warmed digest even after the bytes are
	// replaced on disk, which only holds when the cache is consulted first.
	second, err := service.Inventory(context.Background())
	if err != nil {
		t.Fatalf("second Inventory() returned unexpected error: %v", err)
	}
	if len(first.Files) != 1 || len(second.Files) != 1 {
		t.Fatalf("expected one file in both listings, got %d and %d", len(first.Files), len(second.Files))
	}
	if first.Files[0].SHA256 != second.Files[0].SHA256 {
		t.Fatalf("cached digest changed between calls: %q then %q", first.Files[0].SHA256, second.Files[0].SHA256)
	}
}

// TestDigestForPathComputesOnDemand covers the endpoint that publishing uses
// when the listing omitted a digest.
func TestDigestForPathComputesOnDemand(t *testing.T) {
	service, root := newTestService(t)
	directory := filepath.Join(root, "0.42.0", "stable", "any", "docs")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	payload := []byte("fresh artifact")
	if err := os.WriteFile(filepath.Join(directory, "note.txt"), payload, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	sum := sha256.Sum256(payload)

	digest, err := service.DigestForPath(context.Background(), "0.42.0/stable/any/docs/note.txt")
	if err != nil {
		t.Fatalf("DigestForPath() returned unexpected error: %v", err)
	}
	if digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("unexpected digest: got %q", digest)
	}
	if _, err := service.DigestForPath(context.Background(), "../escape.txt"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}
