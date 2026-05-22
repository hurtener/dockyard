package buildpkg

import (
	"fmt"
	"runtime"
)

// hostTarget returns the Target for the platform the `dockyard` binary itself
// is running on — the fast inner-loop build when no explicit matrix is given.
func hostTarget() Target {
	return Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
}

// Target is one cross-compile target — a GOOS/GOARCH pair. It names the
// platform a `dockyard build` artifact is produced for.
type Target struct {
	// OS is the GOOS value (e.g. "linux", "darwin", "windows").
	OS string
	// Arch is the GOARCH value (e.g. "amd64", "arm64").
	Arch string
}

// String renders a Target as the conventional "os/arch" triple.
func (t Target) String() string { return t.OS + "/" + t.Arch }

// validate checks a Target names a non-empty OS and Arch.
func (t Target) validate() error {
	if t.OS == "" || t.Arch == "" {
		return fmt.Errorf("%w: target %q has an empty OS or Arch", ErrBuild, t)
	}
	return nil
}

// binarySuffix is the executable file-name suffix for the target — ".exe" on
// Windows, empty elsewhere.
func (t Target) binarySuffix() string {
	if t.OS == "windows" {
		return ".exe"
	}
	return ""
}

// DefaultMatrix returns the RFC §14 cross-compile matrix: the
// darwin/linux/windows × amd64/arm64 set every `dockyard build` produces an
// artifact and a checksum for. The order is stable so a Result and the emitted
// dist/ tree are deterministic.
func DefaultMatrix() []Target {
	oses := []string{"darwin", "linux", "windows"}
	arches := []string{"amd64", "arm64"}
	out := make([]Target, 0, len(oses)*len(arches))
	for _, os := range oses {
		for _, arch := range arches {
			out = append(out, Target{OS: os, Arch: arch})
		}
	}
	return out
}
