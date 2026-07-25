package update

import (
	"strings"
	"testing"
)

func TestIsHomebrewPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/opt/homebrew/Cellar/ttt/0.2.0/bin/ttt", true},
		{"/usr/local/Cellar/ttt/0.2.0/bin/ttt", true},
		{"/opt/homebrew/bin/ttt", true},
		{"/home/linuxbrew/.linuxbrew/bin/ttt", true},
		{"/usr/bin/ttt", false},
		{"/usr/local/bin/ttt", false},
		{"/home/user/.local/bin/ttt", false},
		{"/home/user/go/bin/ttt", false},
	}
	for _, tt := range tests {
		if got := isHomebrewPath(tt.path); got != tt.want {
			t.Errorf("isHomebrewPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// fakeOwner substitutes fileOwnedBy so only the given tool claims the file.
func fakeOwner(t *testing.T, owner string) {
	t.Helper()
	orig := fileOwnedBy
	fileOwnedBy = func(tool string, args ...string) bool { return tool == owner }
	t.Cleanup(func() { fileOwnedBy = orig })
}

func TestDetectSourceForPath(t *testing.T) {
	t.Run("brew wins by path alone", func(t *testing.T) {
		fakeOwner(t, "") // no package manager claims it
		if got := detectSourceForPath("/opt/homebrew/Cellar/ttt/0.2.0/bin/ttt", "darwin"); got != SourceHomebrew {
			t.Errorf("got %v, want %v", got, SourceHomebrew)
		}
	})

	for _, tt := range []struct {
		tool string
		want Source
	}{
		{"apk", SourceAPK},
		{"dpkg", SourceDeb},
		{"rpm", SourceRPM},
	} {
		t.Run(tt.tool+" owns the binary", func(t *testing.T) {
			fakeOwner(t, tt.tool)
			if got := detectSourceForPath("/usr/bin/ttt", "linux"); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("package managers not consulted off linux", func(t *testing.T) {
		fakeOwner(t, "dpkg")
		if got := detectSourceForPath("/usr/local/bin/ttt", "darwin"); got != SourceManual {
			t.Errorf("got %v, want %v", got, SourceManual)
		}
	})

	t.Run("unowned file is manual", func(t *testing.T) {
		fakeOwner(t, "")
		if got := detectSourceForPath("/home/user/.local/bin/ttt", "linux"); got != SourceManual {
			t.Errorf("got %v, want %v", got, SourceManual)
		}
	})
}

func TestManagedAndHints(t *testing.T) {
	if SourceManual.Managed() {
		t.Error("manual install must not be managed")
	}
	for _, s := range []Source{SourceHomebrew, SourceAPK, SourceDeb, SourceRPM} {
		if !s.Managed() {
			t.Errorf("%v must be managed", s)
		}
		if s.UpdateHint() == "" || s.UpdateHint() == "ttt update" {
			t.Errorf("%v needs a package-manager update hint, got %q", s, s.UpdateHint())
		}
		err := BlockedError(s)
		if !strings.Contains(err.Error(), s.UpdateHint()) {
			t.Errorf("BlockedError(%v) = %q, want it to include the hint %q", s, err, s.UpdateHint())
		}
	}
}
