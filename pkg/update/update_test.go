package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releaseServer stands in for GitHub: the /repos/…/releases/latest endpoint and
// the two release assets scripts/install.sh also downloads. Every test drives
// the real code path — including the checksum check — against it.
type releaseServer struct {
	tag      string
	archive  []byte
	checksum string // overrides the real sum when non-empty, to forge a mismatch
	asset    string // overrides the name checksums.txt lists, to forge a miss
}

func newReleaseServer(t *testing.T, s releaseServer) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			fmt.Fprintf(w, `{"tag_name": %q}`, s.tag)

		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			_, _ = w.Write(s.archive)

		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			sum := s.checksum
			if sum == "" {
				raw := sha256.Sum256(s.archive)
				sum = hex.EncodeToString(raw[:])
			}

			name := s.asset
			if name == "" {
				name = "lazyshell_linux_amd64.tar.gz"
			}

			fmt.Fprintf(w, "%s  %s\n", sum, name)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

// tarGz builds a release archive holding a single file, the way goreleaser's
// tar.gz does.
func tarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o755,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("tar header: %v", err)
	}

	if _, err := tw.Write(content); err != nil {
		t.Fatalf("tar write: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	return buf.Bytes()
}

// testUpdater is an Updater pinned to a fake GitHub and a fake platform, so the
// archive name is stable whatever box the tests run on.
func testUpdater(srv *httptest.Server, target string) Updater {
	return Updater{
		Repo:         "owner/lazyshell",
		APIBase:      srv.URL,
		DownloadBase: srv.URL,
		GOOS:         "linux",
		GOARCH:       "amd64",
		Client:       srv.Client(),
		Target:       target,
	}
}

func TestLatestReadsTheTag(t *testing.T) {
	srv := newReleaseServer(t, releaseServer{tag: "v1.4.0"})

	got, err := testUpdater(srv, "").Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}

	if got != "v1.4.0" {
		t.Errorf("Latest = %q, want v1.4.0", got)
	}
}

// The whole command in one go: resolve, download, verify, replace — and the
// installed file must be exactly the archive's binary, executable, at the same
// path as before.
func TestDownloadAndInstallReplacesTheBinary(t *testing.T) {
	bin := []byte("#!/bin/sh\necho new\n")
	srv := newReleaseServer(t, releaseServer{tag: "v1.4.0", archive: tarGz(t, "lazyshell", bin)})

	target := filepath.Join(t.TempDir(), "lazyshell")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	u := testUpdater(srv, target)

	got, err := u.Download(context.Background(), "v1.4.0")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	path, err := u.Install(got)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if path != target {
		t.Errorf("Install path = %q, want %q", path, target)
	}

	installed, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed: %v", err)
	}

	if !bytes.Equal(installed, bin) {
		t.Errorf("installed = %q, want %q", installed, bin)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat installed: %v", err)
	}

	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("installed mode = %v, want the owner-executable bit set", info.Mode().Perm())
	}
}

// Install must leave nothing behind in the target's directory: a temporary file
// surviving an update would sit next to the binary, executable, forever.
func TestInstallLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "lazyshell")

	if _, err := (Updater{Target: target}).Install([]byte("bin")); err != nil {
		t.Fatalf("Install: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	if len(entries) != 1 || entries[0].Name() != "lazyshell" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}

		t.Errorf("directory holds %v, want only lazyshell", names)
	}
}

// The point of the checksum step: a tampered archive must never reach the disk.
func TestDownloadRejectsABadChecksum(t *testing.T) {
	srv := newReleaseServer(t, releaseServer{
		tag:      "v1.4.0",
		archive:  tarGz(t, "lazyshell", []byte("payload")),
		checksum: strings.Repeat("0", 64),
	})

	_, err := testUpdater(srv, "").Download(context.Background(), "v1.4.0")
	if err == nil {
		t.Fatal("Download accepted an archive whose checksum does not match")
	}

	if !strings.Contains(err.Error(), "contrôle") {
		t.Errorf("err = %v, want it to name the checksum mismatch", err)
	}
}

// An asset checksums.txt says nothing about is the shape a substituted download
// would take, so it must fail rather than skip the check.
func TestDownloadRejectsAnUnlistedArchive(t *testing.T) {
	srv := newReleaseServer(t, releaseServer{
		tag:     "v1.4.0",
		archive: tarGz(t, "lazyshell", []byte("payload")),
		asset:   "lazyshell_linux_arm64.tar.gz",
	})

	_, err := testUpdater(srv, "").Download(context.Background(), "v1.4.0")
	if err == nil {
		t.Fatal("Download accepted an archive checksums.txt does not mention")
	}
}

func TestDownloadRejectsAnArchiveWithoutTheBinary(t *testing.T) {
	srv := newReleaseServer(t, releaseServer{
		tag:     "v1.4.0",
		archive: tarGz(t, "README.md", []byte("not a binary")),
	})

	_, err := testUpdater(srv, "").Download(context.Background(), "v1.4.0")
	if err == nil {
		t.Fatal("Download accepted an archive with no lazyshell binary in it")
	}
}

func TestArchiveNameRejectsAnUnsupportedPlatform(t *testing.T) {
	if _, err := (Updater{GOOS: "windows", GOARCH: "amd64"}).ArchiveName(); err == nil {
		t.Fatal("ArchiveName accepted windows/amd64, which has no release build")
	}

	got, err := (Updater{GOOS: "darwin", GOARCH: "arm64"}).ArchiveName()
	if err != nil {
		t.Fatalf("ArchiveName(darwin/arm64): %v", err)
	}

	if got != "lazyshell_darwin_arm64.tar.gz" {
		t.Errorf("ArchiveName = %q, want lazyshell_darwin_arm64.tar.gz", got)
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b       string
		want       int
		comparable bool
	}{
		{"v1.2.0", "v1.3.0", -1, true},
		{"v1.3.0", "v1.2.0", 1, true},
		{"v1.2.0", "v1.2.0", 0, true},
		{"1.2.0", "v1.2.0", 0, true},
		{"v1.2", "v1.2.0", 0, true},
		{"v1.9.0", "v1.10.0", -1, true},
		{"v2.0.0", "v1.99.99", 1, true},
		{"dev", "v1.2.0", 0, false},
		{"v1.2.0-rc1", "v1.2.0", 0, false},
		// What `make build` produces from a tagged checkout with commits on top.
		{"v1.1.0-3-gabc1234", "v1.2.0", 0, false},
		{"", "v1.2.0", 0, false},
	}

	for _, tc := range cases {
		got, ok := Compare(tc.a, tc.b)
		if ok != tc.comparable || (ok && got != tc.want) {
			t.Errorf("Compare(%q, %q) = %d, %v; want %d, %v", tc.a, tc.b, got, ok, tc.want, tc.comparable)
		}
	}
}

func TestIsRelease(t *testing.T) {
	for _, v := range []string{"v1.2.0", "1.2.0", "v0.1"} {
		if !IsRelease(v) {
			t.Errorf("IsRelease(%q) = false, want true", v)
		}
	}

	for _, v := range []string{"dev", "", "v1.2.0-rc1", "nonsense"} {
		if IsRelease(v) {
			t.Errorf("IsRelease(%q) = true, want false", v)
		}
	}
}
