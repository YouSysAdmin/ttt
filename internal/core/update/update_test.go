package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "1.0.0", "1.0.0", 0},
		{"equal with v prefix", "v1.0.0", "1.0.0", 0},
		{"a less major", "1.0.0", "2.0.0", -1},
		{"a less minor", "1.1.0", "1.2.0", -1},
		{"a less patch", "1.0.1", "1.0.2", -1},
		{"a greater major", "3.0.0", "2.9.9", 1},
		{"a greater minor", "1.2.0", "1.1.9", 1},
		{"a greater patch", "1.0.5", "1.0.4", 1},
		{"pre-release stripped", "2.0.0-pre", "1.9.9", 1},
		{"invalid a treated as 0.0.0", "invalid", "0.0.1", -1},
		{"invalid b treated as 0.0.0", "0.0.1", "invalid", 1},
		{"both invalid", "bad", "worse", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareVersions(tt.a, tt.b); got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name                      string
		input                     string
		wantMaj, wantMin, wantPat int
		wantErr                   bool
	}{
		{"simple", "1.2.3", 1, 2, 3, false},
		{"with v", "v3.0.1", 3, 0, 1, false},
		{"pre-release", "2.0.0-pre", 2, 0, 0, false},
		{"too few parts", "1.2", 0, 0, 0, true},
		{"non-numeric major", "a.2.3", 0, 0, 0, true},
		{"empty", "", 0, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maj, min, pat, err := parseVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err == nil && (maj != tt.wantMaj || min != tt.wantMin || pat != tt.wantPat) {
				t.Errorf("parseVersion(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tt.input, maj, min, pat, tt.wantMaj, tt.wantMin, tt.wantPat)
			}
		})
	}
}

// Asset names must match the .goreleaser.yml name_template.
func TestBuildAssetName(t *testing.T) {
	tests := []struct {
		name, version, goos, goarch, want string
	}{
		{"linux amd64", "0.2.0", "linux", "amd64", "ttt_v0.2.0_linux_amd64.tar.gz"},
		{"linux arm64", "0.2.0", "linux", "arm64", "ttt_v0.2.0_linux_arm64.tar.gz"},
		{"linux 386", "0.2.0", "linux", "386", "ttt_v0.2.0_linux_i386.tar.gz"},
		{"linux arm", "0.2.0", "linux", "arm", "ttt_v0.2.0_linux_armv7.tar.gz"},
		{"darwin arm64", "0.2.0", "darwin", "arm64", "ttt_v0.2.0_darwin_arm64.tar.gz"},
		{"windows amd64", "0.2.0", "windows", "amd64", "ttt_v0.2.0_windows_amd64.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildAssetName(tt.version, tt.goos, tt.goarch); got != tt.want {
				t.Errorf("BuildAssetName(%q, %q, %q) = %q, want %q",
					tt.version, tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

// withReleaseAPI points ReleaseAPIURL at a test server for the test's duration.
func withReleaseAPI(t *testing.T, url string) {
	t.Helper()
	orig := ReleaseAPIURL
	ReleaseAPIURL = url
	t.Cleanup(func() { ReleaseAPIURL = orig })
}

func TestCheckLatestVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{TagName: "v2.0.0"})
	}))
	defer srv.Close()
	withReleaseAPI(t, srv.URL)

	t.Run("newer available", func(t *testing.T) {
		res := CheckLatestVersion("1.0.0")
		if res.Err != nil {
			t.Fatalf("unexpected error: %v", res.Err)
		}
		if res.LatestVersion != "2.0.0" {
			t.Errorf("LatestVersion = %q, want %q", res.LatestVersion, "2.0.0")
		}
	})

	t.Run("up to date", func(t *testing.T) {
		res := CheckLatestVersion("2.0.0")
		if res.Err != nil {
			t.Fatalf("unexpected error: %v", res.Err)
		}
		if res.LatestVersion != "" {
			t.Errorf("LatestVersion = %q, want empty (up to date)", res.LatestVersion)
		}
	})

	t.Run("dev build skipped", func(t *testing.T) {
		for _, v := range []string{"", "dev"} {
			res := CheckLatestVersion(v)
			if res.Err != nil || res.LatestVersion != "" {
				t.Errorf("CheckLatestVersion(%q) = %+v, want empty result", v, res)
			}
		}
	})
}

func TestCheckLatestVersion_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	withReleaseAPI(t, srv.URL)

	if res := CheckLatestVersion("1.0.0"); res.Err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestVerifyChecksum(t *testing.T) {
	content := []byte("test binary content for checksum verification")
	archivePath := writeTempFile(t, content)

	h := sha256.Sum256(content)
	checksums := fmt.Sprintf("%s  ttt_v1.0.0_linux_amd64.tar.gz\n", hex.EncodeToString(h[:]))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksums)
	}))
	defer srv.Close()

	assets := []Asset{{Name: "checksums.sha256", BrowserDownloadURL: srv.URL}}

	t.Run("valid", func(t *testing.T) {
		if err := verifyChecksum(assets, archivePath, "ttt_v1.0.0_linux_amd64.tar.gz"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("missing entry", func(t *testing.T) {
		if err := verifyChecksum(assets, archivePath, "nonexistent.tar.gz"); err == nil {
			t.Fatal("expected error for missing checksum entry")
		}
	})
	t.Run("tampered file", func(t *testing.T) {
		tampered := writeTempFile(t, []byte("tampered content"))
		err := verifyChecksum(assets, tampered, "ttt_v1.0.0_linux_amd64.tar.gz")
		if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("want checksum mismatch error, got %v", err)
		}
	})
	t.Run("no checksums asset", func(t *testing.T) {
		if err := verifyChecksum(nil, archivePath, "ttt_v1.0.0_linux_amd64.tar.gz"); err == nil {
			t.Fatal("expected error for missing checksums.sha256 asset")
		}
	})
}

func TestExtractArchives(t *testing.T) {
	binary := []byte("#!/bin/sh\necho hello\n")

	t.Run("tar.gz", func(t *testing.T) {
		path := writeTempFile(t, tarGzBytes(t, "ttt", binary))
		data, err := extractTarGz(path, "ttt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(data, binary) {
			t.Errorf("extracted %q, want %q", data, binary)
		}
		if _, err := extractTarGz(path, "nonexistent"); err == nil {
			t.Fatal("expected error for missing binary")
		}
	})

	t.Run("zip", func(t *testing.T) {
		path := writeTempFile(t, zipBytes(t, "ttt.exe", binary))
		data, err := extractZip(path, "ttt.exe")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(data, binary) {
			t.Errorf("extracted %q, want %q", data, binary)
		}
		if _, err := extractZip(path, "nonexistent"); err == nil {
			t.Fatal("expected error for missing binary")
		}
	})
}

func TestAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "ttt")
	if err := os.WriteFile(exePath, []byte("old binary"), 0755); err != nil {
		t.Fatal(err)
	}

	newContent := []byte("new binary v2")
	if err := atomicReplace(exePath, newContent); err != nil {
		t.Fatalf("atomicReplace: %v", err)
	}

	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newContent) {
		t.Errorf("replaced content = %q, want %q", got, newContent)
	}
	if _, err := os.Stat(exePath + ".old"); !os.IsNotExist(err) {
		t.Errorf(".old file still exists")
	}
	info, err := os.Stat(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("permissions = %o, want 0755", info.Mode().Perm())
	}
}

func TestDownloadAndReplace(t *testing.T) {
	fakeBinary := []byte("#!/bin/sh\necho updated ttt\n")
	binName := "ttt"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}

	assetName := BuildAssetName("2.0.0", runtime.GOOS, runtime.GOARCH)
	var archiveData []byte
	if runtime.GOOS == "windows" {
		archiveData = zipBytes(t, binName, fakeBinary)
	} else {
		archiveData = tarGzBytes(t, binName, fakeBinary)
	}
	archiveHash := sha256.Sum256(archiveData)
	checksumLine := fmt.Sprintf("%s  %s\n", hex.EncodeToString(archiveHash[:]), assetName)

	dir := t.TempDir()
	exe := filepath.Join(dir, binName)
	resetBinary := func() {
		if err := os.WriteFile(exe, []byte("old binary"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	resetBinary()

	origExecPath := execPath
	execPath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { execPath = origExecPath })

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/release", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(Release{
			TagName: "v2.0.0",
			Assets: []Asset{
				{Name: assetName, BrowserDownloadURL: srv.URL + "/archive"},
				{Name: "checksums.sha256", BrowserDownloadURL: srv.URL + "/checksums"},
			},
		})
	})
	mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) { w.Write(archiveData) })
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, checksumLine) })
	withReleaseAPI(t, srv.URL+"/release")

	t.Run("already up to date", func(t *testing.T) {
		var buf bytes.Buffer
		latest, err := DownloadAndReplace("2.0.0", &buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if latest != "2.0.0" {
			t.Errorf("latest = %q, want %q even when up to date", latest, "2.0.0")
		}
		if !strings.Contains(buf.String(), "Already up to date") {
			t.Errorf("output = %q, want 'Already up to date'", buf.String())
		}
	})

	t.Run("update available", func(t *testing.T) {
		resetBinary()
		var buf bytes.Buffer
		latest, err := DownloadAndReplace("1.0.0", &buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if latest != "2.0.0" {
			t.Errorf("latest = %q, want %q", latest, "2.0.0")
		}
		if !strings.Contains(buf.String(), "Updated successfully") {
			t.Errorf("output = %q, want 'Updated successfully'", buf.String())
		}
		got, err := os.ReadFile(exe)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, fakeBinary) {
			t.Errorf("binary content = %q, want %q", got, fakeBinary)
		}
	})

	t.Run("dev build updates with warning", func(t *testing.T) {
		resetBinary()
		var buf bytes.Buffer
		if _, err := DownloadAndReplace("dev", &buf); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "development build") || !strings.Contains(out, "Updated to v2.0.0") {
			t.Errorf("output = %q, want dev-build warning and 'Updated to v2.0.0'", out)
		}
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func writeTempFile(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func tarGzBytes(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func zipBytes(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	return buf.Bytes()
}
