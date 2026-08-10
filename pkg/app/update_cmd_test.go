package app

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

	"github.com/thomas-gleizes/lazyshell/pkg/update"
)

// fakeGitHub serves the three things `lazyshell update` asks for: the latest
// tag, the archive for linux/amd64, and its checksums.txt.
func fakeGitHub(t *testing.T, tag string, bin []byte) *httptest.Server {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if err := tw.WriteHeader(&tar.Header{
		Name: "lazyshell", Mode: 0o755, Size: int64(len(bin)), Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("tar header: %v", err)
	}

	if _, err := tw.Write(bin); err != nil {
		t.Fatalf("tar write: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	archive := buf.Bytes()
	sum := sha256.Sum256(archive)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			fmt.Fprintf(w, `{"tag_name": %q}`, tag)
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			_, _ = w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			fmt.Fprintf(w, "%s  lazyshell_linux_amd64.tar.gz\n", hex.EncodeToString(sum[:]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func updaterFor(srv *httptest.Server, target string) update.Updater {
	return update.Updater{
		Repo:         "owner/lazyshell",
		APIBase:      srv.URL,
		DownloadBase: srv.URL,
		GOOS:         "linux",
		GOARCH:       "amd64",
		Client:       srv.Client(),
		Target:       target,
	}
}

// seedBinary is the "already installed" file every test replaces (or doesn't).
func seedBinary(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "lazyshell")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed binary: %v", err)
	}

	return path
}

func TestRunUpdateInstallsANewerRelease(t *testing.T) {
	target := seedBinary(t)
	srv := fakeGitHub(t, "v1.4.0", []byte("new binary"))

	var out bytes.Buffer
	if err := runUpdate(context.Background(), updaterFor(srv, target), "v1.3.0", Options{}, &out); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	installed, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read installed: %v", err)
	}

	if string(installed) != "new binary" {
		t.Errorf("installed = %q, want the release binary", installed)
	}

	if !strings.Contains(out.String(), "v1.4.0") {
		t.Errorf("out = %q, want it to name the installed version", out.String())
	}
}

// The common case for anyone typing the command out of habit: nothing newer,
// nothing downloaded, nothing replaced, and it says so rather than failing.
func TestRunUpdateUpToDateChangesNothing(t *testing.T) {
	target := seedBinary(t)
	srv := fakeGitHub(t, "v1.4.0", []byte("new binary"))

	var out bytes.Buffer
	if err := runUpdate(context.Background(), updaterFor(srv, target), "v1.4.0", Options{}, &out); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}

	if !strings.Contains(out.String(), "déjà à jour") {
		t.Errorf("out = %q, want it to say the install is up to date", out.String())
	}

	installed, _ := os.ReadFile(target)
	if string(installed) != "old" {
		t.Errorf("installed = %q, want the binary left alone", installed)
	}
}

func TestRunUpdateForceReinstallsAnUpToDateBinary(t *testing.T) {
	target := seedBinary(t)
	srv := fakeGitHub(t, "v1.4.0", []byte("new binary"))

	var out bytes.Buffer
	if err := runUpdate(context.Background(), updaterFor(srv, target), "v1.4.0", Options{Force: true}, &out); err != nil {
		t.Fatalf("runUpdate --force: %v", err)
	}

	installed, _ := os.ReadFile(target)
	if string(installed) != "new binary" {
		t.Errorf("installed = %q, want --force to reinstall", installed)
	}
}

// --check is read-only, whatever it finds.
func TestRunUpdateCheckNeverInstalls(t *testing.T) {
	for _, tc := range []struct{ current, want string }{
		{"v1.3.0", "mise à jour est disponible"},
		{"v1.4.0", "déjà à jour"},
		{"dev", "n'est pas une version publiée"},
	} {
		target := seedBinary(t)
		srv := fakeGitHub(t, "v1.4.0", []byte("new binary"))

		var out bytes.Buffer
		if err := runUpdate(context.Background(), updaterFor(srv, target), tc.current, Options{Check: true}, &out); err != nil {
			t.Fatalf("runUpdate --check (%s): %v", tc.current, err)
		}

		if !strings.Contains(out.String(), tc.want) {
			t.Errorf("--check with %s printed %q, want it to mention %q", tc.current, out.String(), tc.want)
		}

		if installed, _ := os.ReadFile(target); string(installed) != "old" {
			t.Errorf("--check with %s replaced the binary", tc.current)
		}
	}
}

// A locally built binary ("dev", or `git describe`'s output) is never silently
// overwritten: the whole point of having built it is that it is not a release.
func TestRunUpdateRefusesToOverwriteALocalBuild(t *testing.T) {
	target := seedBinary(t)
	srv := fakeGitHub(t, "v1.4.0", []byte("new binary"))

	var out bytes.Buffer

	err := runUpdate(context.Background(), updaterFor(srv, target), "dev", Options{}, &out)
	if err == nil {
		t.Fatal("runUpdate replaced a dev build without --force")
	}

	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("err = %v, want it to point at --force", err)
	}

	if installed, _ := os.ReadFile(target); string(installed) != "old" {
		t.Errorf("installed = %q, want the dev build left alone", installed)
	}

	out.Reset()

	if err := runUpdate(context.Background(), updaterFor(srv, target), "dev", Options{Force: true}, &out); err != nil {
		t.Fatalf("runUpdate --force on a dev build: %v", err)
	}

	if installed, _ := os.ReadFile(target); string(installed) != "new binary" {
		t.Errorf("installed = %q, want --force to install over a dev build", installed)
	}
}

// An unsupported platform is a refusal, not a 404 halfway through a download.
func TestRunUpdateRejectsAnUnsupportedPlatformBeforeAnyRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++

		http.NotFound(w, r)
	}))
	defer srv.Close()

	u := updaterFor(srv, seedBinary(t))
	u.GOOS = "windows"

	var out bytes.Buffer
	if err := runUpdate(context.Background(), u, "v1.3.0", Options{}, &out); err == nil {
		t.Fatal("runUpdate accepted windows, which has no release build")
	}

	if requests != 0 {
		t.Errorf("%d request(s) made, want none before the platform check", requests)
	}
}

func TestParseArgsUpdate(t *testing.T) {
	cases := []struct {
		args  []string
		check bool
		force bool
	}{
		{[]string{"update"}, false, false},
		{[]string{"update", "--check"}, true, false},
		{[]string{"update", "--force"}, false, true},
	}

	for _, tc := range cases {
		inv, err := ParseArgs(tc.args)
		if err != nil {
			t.Fatalf("ParseArgs(%v): %v", tc.args, err)
		}

		if inv.Command != CommandUpdate {
			t.Errorf("ParseArgs(%v).Command = %q, want %q", tc.args, inv.Command, CommandUpdate)
		}

		if inv.Check != tc.check || inv.Force != tc.force {
			t.Errorf("ParseArgs(%v) = check %v, force %v; want %v, %v",
				tc.args, inv.Check, inv.Force, tc.check, tc.force)
		}
	}

	if _, err := ParseArgs([]string{"update", "now"}); err == nil {
		t.Error("ParseArgs accepted a positional argument to update")
	}
}
