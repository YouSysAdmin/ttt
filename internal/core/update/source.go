package update

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Source identifies how the running ttt binary was installed.
type Source string

const (
	SourceManual   Source = "manual" // go install, install.sh, hand-copied
	SourceHomebrew Source = "brew"
	SourceAPK      Source = "apk"
	SourceDeb      Source = "deb"
	SourceRPM      Source = "rpm"
)

// Managed reports whether the install is owned by a package manager, in
// which case self-update must be blocked: replacing the binary behind the
// manager's back would be undone by its next upgrade and breaks its file
// database.
func (s Source) Managed() bool {
	return s != SourceManual
}

// UpdateHint returns the command the user should run instead of `ttt update`
// for a package-managed install.
func (s Source) UpdateHint() string {
	switch s {
	case SourceHomebrew:
		return "brew upgrade ttt"
	case SourceAPK:
		return "apk upgrade ttt"
	case SourceDeb:
		return "apt-get install --only-upgrade ttt"
	case SourceRPM:
		return "dnf upgrade ttt (or yum update ttt)"
	default:
		return "ttt update"
	}
}

// String returns a human-readable name for error messages.
func (s Source) String() string {
	switch s {
	case SourceHomebrew:
		return "Homebrew"
	case SourceAPK:
		return "apk package"
	case SourceDeb:
		return "deb package"
	case SourceRPM:
		return "rpm package"
	default:
		return "manual install"
	}
}

// DetectSource determines how the running binary was installed: by its
// resolved path for Homebrew, and by asking each present package manager
// whether it owns the file for apk/deb/rpm. Unknown stays SourceManual —
// self-update is only blocked on positive evidence.
func DetectSource() (Source, error) {
	exe, err := execPath()
	if err != nil {
		return SourceManual, err
	}
	return detectSourceForPath(exe, runtime.GOOS), nil
}

func detectSourceForPath(exe, goos string) Source {
	if isHomebrewPath(exe) {
		return SourceHomebrew
	}
	if goos == "linux" {
		// Ask each package manager whether it owns the binary. The tools
		// exit non-zero for files they don't own, so a zero exit is a claim.
		if fileOwnedBy("apk", "info", "--who-owns", exe) {
			return SourceAPK
		}
		if fileOwnedBy("dpkg", "-S", exe) {
			return SourceDeb
		}
		if fileOwnedBy("rpm", "-qf", exe) {
			return SourceRPM
		}
	}
	return SourceManual
}

// isHomebrewPath reports whether the resolved executable lives in a Homebrew
// prefix (macOS /opt/homebrew or /usr/local/Cellar, Linuxbrew ~/.linuxbrew).
func isHomebrewPath(exe string) bool {
	for _, marker := range []string{"/Cellar/", "/homebrew/", "/.linuxbrew/"} {
		if strings.Contains(exe, marker) {
			return true
		}
	}
	return false
}

// fileOwnedBy runs a package manager's file-ownership query. A var so tests
// can fake manager responses.
var fileOwnedBy = func(tool string, args ...string) bool {
	if _, err := exec.LookPath(tool); err != nil {
		return false
	}
	return exec.Command(tool, args...).Run() == nil
}

// BlockedError explains why self-update is unavailable and what to run
// instead. Raised by callers when DetectSource reports a managed install.
func BlockedError(s Source) error {
	return fmt.Errorf("this ttt was installed as a %s; self-update is disabled.\nUpdate it with: %s", s, s.UpdateHint())
}
