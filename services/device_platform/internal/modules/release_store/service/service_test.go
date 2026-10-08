package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
