// Package buildinfo exposes the build-time version without exposing runtime
// configuration or provider details.
package buildinfo

// Version is replaced at link time with the release version. The development
// default keeps local health responses unambiguous.
var Version = "dev"

// Current returns the injected release version or the local development
// marker when the binary was not built by the release pipeline.
func Current() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
