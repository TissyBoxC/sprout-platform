// Package domain defines the download-store inventory and index contract.
//
// The download store is the shared release volume mounted by download_http.
// Device platform only exposes metadata and controlled writes; it never
// serves file bytes, so the public download service remains the sole reader.
package domain

import (
	"errors"
	"time"
)

var (
	ErrStoreUnavailable   = errors.New("download store unavailable")
	ErrPathInvalid        = errors.New("download path invalid")
	ErrFileNotFound       = errors.New("download file not found")
	ErrFileAlreadyExists  = errors.New("download file already exists")
	ErrFileTooLarge       = errors.New("download file too large")
	ErrReleaseInvalid     = errors.New("download release manifest invalid")
	ErrReleaseNotCreated  = errors.New("download release directory not found")
	ErrArtifactNotFound   = errors.New("download artifact not found")
	ErrUnsupportedChannel = errors.New("download channel unsupported")
	ErrArtifactMismatch   = errors.New("download artifact digest mismatch")
	ErrFileImmutable      = errors.New("published download file is immutable")
)

const (
	// MaxUploadBytes keeps a single operator upload below the reverse proxy
	// and disk-safety limits. Larger releases must be published by CI.
	MaxUploadBytes int64 = 512 * 1024 * 1024

	DefaultChannel = "stable"

	// Canonical release artifact kinds differ from the operator-facing
	// registration kinds: the package directory uses the transport format
	// (apk/admin-web) while platform_releases stores the delivery action
	// (client/resource). Keeping the mapping here prevents the download panel
	// and the release registration flow from drifting apart.
	ArtifactKindAPK       = "apk"
	ArtifactKindAdminWeb  = "admin-web"
	ArtifactKindContracts = "contracts"
	ArtifactKindBinary    = "binary"
	ArtifactKindFirmware  = "firmware"
	ArtifactKindResource  = "resource"
	ArtifactKindClient    = "client"
)

// File is one real file in the shared download volume.
//
// Release is nil when the path is not a canonical
// <version>/<channel>/<platform>/<kind>/<filename> artifact. Operators can
// still see and manage such files, but index generation ignores them.
type File struct {
	RelativePath string       `json:"relative_path"`
	FileName     string       `json:"file_name"`
	Name         string       `json:"name"`
	Directory    string       `json:"directory"`
	SizeBytes    int64        `json:"size_bytes"`
	ModifiedAt   time.Time    `json:"modified_at"`
	SHA256       string       `json:"sha256"`
	Release      *FileRelease `json:"release"`
	DownloadURL  string       `json:"download_url"`
	IsIndexed    bool         `json:"is_indexed"`
}

// FileRelease links a canonical artifact path to its version metadata.
type FileRelease struct {
	Version  string `json:"version"`
	Channel  string `json:"channel"`
	Platform string `json:"platform"`
	Kind     string `json:"kind"`
}

// RegistrationKind maps a canonical artifact directory to the release kind
// used by platform_releases. Unknown values are returned unchanged so the
// operator can still inspect and register newer artifact formats.
func (r *FileRelease) RegistrationKind() string {
	if r == nil {
		return ""
	}
	switch r.Kind {
	case ArtifactKindAPK, ArtifactKindAdminWeb:
		return ArtifactKindClient
	case ArtifactKindContracts, ArtifactKindResource:
		return ArtifactKindResource
	default:
		return r.Kind
	}
}

// UploadInput is one validated manual upload.
//
// RelativePath is relative to the shared root and must include the file name.
// Existing files are replaced atomically only when Overwrite is true.
type UploadInput struct {
	RelativePath string
	Overwrite    bool
	Version      string
	Channel      string
	Platform     string
	Kind         string
	FileName     string
}

// IndexRefreshRequest regenerates one release manifest and the global index.
//
// When Title or PublishedAt are empty, the service preserves an existing
// manifest value or uses a deterministic default. PublicBaseURL is never
// accepted from the request; it comes from server configuration.
type IndexRefreshRequest struct {
	Version     string
	Channel     string
	Title       string
	PublishedAt string
}

// IndexRefreshResult reports the generated release and the artifact count.
type IndexRefreshResult struct {
	Version       string    `json:"version"`
	Channel       string    `json:"channel"`
	ArtifactCount int       `json:"artifact_count"`
	ManifestURL   string    `json:"manifest_url"`
	GeneratedAt   time.Time `json:"generated_at"`
}

// IndexRefreshSummary describes a full store scan. Versions and Channels list
// every release directory that was regenerated during the scan.
type IndexRefreshSummary struct {
	RefreshedAt      time.Time `json:"refreshed_at"`
	ReleaseCount     int       `json:"release_count"`
	ArtifactCount    int       `json:"artifact_count"`
	IndexedFileCount int       `json:"indexed_file_count"`
	PendingFileCount int       `json:"pending_file_count"`
	Versions         []string  `json:"versions"`
	Channels         []string  `json:"channels"`
}

// IndexStatus reports whether every canonical artifact is represented in the
// public manifest and index. The console uses it to surface drift without
// blocking uploads when CI owns the index.
type IndexStatus struct {
	IsAvailable      bool      `json:"is_available"`
	RefreshedAt      time.Time `json:"refreshed_at"`
	IndexedFileCount int       `json:"indexed_file_count"`
	PendingFileCount int       `json:"pending_file_count"`
	ErrorMessage     string    `json:"error_message"`
}

// Inventory is the console-facing download-store snapshot.
type Inventory struct {
	RootDir       string    `json:"root_dir"`
	PublicBaseURL string    `json:"public_base_url"`
	Files         []File    `json:"files"`
	GeneratedAt   time.Time `json:"generated_at"`
}

// Manifest mirrors tools/release_artifacts.py so CI and the management API
// generate byte-compatible public metadata.
type Manifest struct {
	Artifacts []ManifestArtifact `json:"artifacts"`
	Channel   string             `json:"channel"`
	Version   string             `json:"version"`
}

// ManifestArtifact is one downloadable file in a release manifest.
type ManifestArtifact struct {
	FileName     string `json:"file_name"`
	Kind         string `json:"kind"`
	Platform     string `json:"platform"`
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256"`
	SizeBytes    int64  `json:"size_bytes"`
	URL          string `json:"url"`
}

// ReleaseIndex mirrors the global index.json generated by CI.
type ReleaseIndex struct {
	GeneratedAt   time.Time      `json:"generated_at"`
	Releases      []IndexRelease `json:"releases"`
	SchemaVersion int            `json:"schema_version"`
}

// IndexRelease is one release entry in the global index.
type IndexRelease struct {
	ArtifactCount int    `json:"artifact_count"`
	Channel       string `json:"channel"`
	ManifestURL   string `json:"manifest_url"`
	PublishedAt   string `json:"published_at"`
	Title         string `json:"title"`
	Version       string `json:"version"`
}
