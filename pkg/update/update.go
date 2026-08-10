// Package update implements what `lazyshell update` needs: find the latest
// GitHub release, download the archive built for this OS/arch, check it against
// the release's checksums.txt, and swap the running binary for the one inside.
//
// It is the in-binary equivalent of scripts/install.sh, and deliberately does
// the same three things in the same order — download, verify, replace. The
// checksum step is not optional: this code writes an executable that will be run
// as the user, so a truncated or tampered download must fail loudly rather than
// land on disk.
//
// Nothing here knows about the CLI: pkg/app owns the messages, the flags and the
// "am I newer" decision. This package only knows how to fetch and install.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	// Repo is where releases come from, in "owner/name" form. Same repository
	// scripts/install.sh downloads from.
	Repo = "thomas-gleizes/lazyshell"

	defaultAPIBase      = "https://api.github.com"
	defaultDownloadBase = "https://github.com"

	// binaryName is the file to extract from the archive, and the name
	// goreleaser gives the built binary.
	binaryName = "lazyshell"

	// maxArchiveSize caps what is read from the network and out of the
	// gzip stream. The archive is a few megabytes; this is a bomb guard, not
	// a budget.
	maxArchiveSize = 128 << 20
)

// Updater is a configured "where do I get releases from". The zero value works
// and targets the real GitHub repository for the running OS/arch — every field
// exists so the tests can point the whole thing at an httptest server and a
// temporary directory.
type Updater struct {
	// Repo is the "owner/name" repository, defaulting to Repo.
	Repo string
	// APIBase is the GitHub API root, defaulting to https://api.github.com.
	APIBase string
	// DownloadBase is the release-download root, defaulting to
	// https://github.com.
	DownloadBase string
	// GOOS and GOARCH select the archive, defaulting to the running binary's.
	GOOS, GOARCH string
	// Client is the HTTP client, defaulting to http.DefaultClient.
	Client *http.Client
	// Target is the file to replace, defaulting to the running executable
	// (symlinks resolved).
	Target string
}

func (u Updater) withDefaults() Updater {
	if u.Repo == "" {
		u.Repo = Repo
	}

	if u.APIBase == "" {
		u.APIBase = defaultAPIBase
	}

	if u.DownloadBase == "" {
		u.DownloadBase = defaultDownloadBase
	}

	if u.GOOS == "" {
		u.GOOS = runtime.GOOS
	}

	if u.GOARCH == "" {
		u.GOARCH = runtime.GOARCH
	}

	if u.Client == nil {
		u.Client = http.DefaultClient
	}

	return u
}

// supportedPlatforms mirrors .goreleaser.yml's build matrix. Checked before
// anything is downloaded so an unsupported box gets the real reason rather than
// a 404 on an archive that was never built.
var supportedPlatforms = map[string]bool{
	"linux/amd64":  true,
	"linux/arm64":  true,
	"darwin/amd64": true,
	"darwin/arm64": true,
}

// ArchiveName is the release asset for this platform, per .goreleaser.yml's
// name_template.
func (u Updater) ArchiveName() (string, error) {
	u = u.withDefaults()

	platform := u.GOOS + "/" + u.GOARCH
	if !supportedPlatforms[platform] {
		return "", fmt.Errorf("aucune release pour %s (linux et darwin, amd64 et arm64, uniquement)", platform)
	}

	return fmt.Sprintf("%s_%s_%s.tar.gz", binaryName, u.GOOS, u.GOARCH), nil
}

// Latest returns the tag of the latest published release, e.g. "v1.2.0".
func (u Updater) Latest(ctx context.Context) (string, error) {
	u = u.withDefaults()

	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimSuffix(u.APIBase, "/"), u.Repo)

	body, err := u.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return "", err
	}

	var release struct {
		TagName string `json:"tag_name"`
	}

	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("réponse illisible de %s: %w", url, err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("%s ne publie aucune release", u.Repo)
	}

	return release.TagName, nil
}

// Download fetches the archive for the given tag, checks it against the
// release's checksums.txt and returns the verified binary's bytes. Nothing
// touches the filesystem here: an installation that fails halfway must not be
// able to leave a partial binary behind.
func (u Updater) Download(ctx context.Context, tag string) ([]byte, error) {
	u = u.withDefaults()

	archive, err := u.ArchiveName()
	if err != nil {
		return nil, err
	}

	base := fmt.Sprintf("%s/%s/releases/download/%s", strings.TrimSuffix(u.DownloadBase, "/"), u.Repo, tag)

	blob, err := u.get(ctx, base+"/"+archive, "")
	if err != nil {
		return nil, err
	}

	sums, err := u.get(ctx, base+"/checksums.txt", "")
	if err != nil {
		return nil, err
	}

	if err := verify(blob, sums, archive); err != nil {
		return nil, err
	}

	return extractBinary(blob)
}

// Install replaces the target with bin and returns the path it wrote to.
//
// The write is a temporary file in the target's own directory followed by a
// rename, which is what makes it atomic: an interrupted update leaves either the
// old binary or the new one, never half of either. Renaming over a running
// executable is fine on Unix — the running process keeps the inode it started
// with — which is why `lazyshell update` can be run from inside lazyshell.
func (u Updater) Install(bin []byte) (string, error) {
	path, err := u.targetPath()
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(path)

	// The old binary's permissions win when there is one: an install into a
	// directory with its own conventions (a group-writable /opt, say) should not
	// be quietly normalised to 0755 by an update.
	mode := fs.FileMode(0o755)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm() | 0o100
	}

	tmp, err := os.CreateTemp(dir, "."+binaryName+"-update-*")
	if err != nil {
		return "", writeError(dir, err)
	}

	tmpName := tmp.Name()

	// A no-op once the rename below has succeeded, and the cleanup for every
	// path that returns before it.
	defer os.Remove(tmpName)

	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()

		return "", fmt.Errorf("écriture de %s: %w", tmpName, err)
	}

	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("écriture de %s: %w", tmpName, err)
	}

	if err := os.Chmod(tmpName, mode); err != nil {
		return "", fmt.Errorf("chmod %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return "", writeError(path, err)
	}

	return path, nil
}

// targetPath is the file Install replaces: Target when set, else the running
// executable with its symlinks resolved — updating through a symlink must
// replace the binary it points at, not turn the symlink into a copy.
func (u Updater) targetPath() (string, error) {
	if u.Target != "" {
		return u.Target, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("impossible de localiser le binaire en cours d'exécution: %w", err)
	}

	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	return exe, nil
}

// writeError turns a permission problem into the sentence that says what to do
// about it. A binary in /usr/local/bin is the normal case, and "permission
// denied" on its own sends people to the issue tracker rather than to sudo.
func writeError(path string, err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf("%s n'est pas modifiable : relancez avec « sudo $(command -v %s) update », ou réinstallez via scripts/install.sh",
			path, binaryName)
	}

	return fmt.Errorf("remplacement de %s: %w", path, err)
}

// get fetches a URL and returns its body, capped at maxArchiveSize.
func (u Updater) get(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	req.Header.Set("User-Agent", binaryName)

	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("téléchargement de %s: %w", url, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("%s: %s (quota d'API GitHub atteint, réessayez plus tard)", url, resp.Status)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveSize))
	if err != nil {
		return nil, fmt.Errorf("lecture de %s: %w", url, err)
	}

	return body, nil
}

// verify checks blob against the checksums.txt line naming archive. A missing
// line is a failure, not a skip: "no checksum for this file" is exactly what a
// substituted asset would look like.
func verify(blob, sums []byte, archive string) error {
	want := ""

	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == archive {
			want = fields[0]

			break
		}
	}

	if want == "" {
		return fmt.Errorf("checksums.txt ne mentionne pas %s", archive)
	}

	sum := sha256.Sum256(blob)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, want) {
		return fmt.Errorf("somme de contrôle de %s invalide (attendu %s, obtenu %s)", archive, want, got)
	}

	return nil
}

// extractBinary pulls the lazyshell binary out of the release tarball.
func extractBinary(blob []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, fmt.Errorf("archive illisible: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(io.LimitReader(gz, maxArchiveSize))

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("archive illisible: %w", err)
		}

		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != binaryName {
			continue
		}

		bin, err := io.ReadAll(io.LimitReader(tr, maxArchiveSize))
		if err != nil {
			return nil, fmt.Errorf("archive illisible: %w", err)
		}

		if len(bin) == 0 {
			return nil, errors.New("archive illisible: le binaire " + binaryName + " y est vide")
		}

		return bin, nil
	}

	return nil, fmt.Errorf("archive illisible: aucun binaire %s à l'intérieur", binaryName)
}

// IsRelease reports whether v looks like a version this package can compare —
// i.e. a real release, not the "dev" of a `go build` without ldflags nor the
// `git describe` string a local `make build` produces.
func IsRelease(v string) bool {
	_, ok := parseVersion(v)

	return ok
}

// Compare orders two release versions: -1 if a < b, 0 if equal, 1 if a > b. It
// reports false when either side is not a plain vX.Y.Z — the caller decides what
// to do about a version it cannot place, and guessing would be the one wrong
// answer (a build called "dev" is not "older than everything").
func Compare(a, b string) (int, bool) {
	va, ok := parseVersion(a)
	if !ok {
		return 0, false
	}

	vb, ok := parseVersion(b)
	if !ok {
		return 0, false
	}

	for i := range va {
		switch {
		case va[i] < vb[i]:
			return -1, true
		case va[i] > vb[i]:
			return 1, true
		}
	}

	return 0, true
}

// parseVersion reads "v1.2.3", "1.2" or "v1" into three numbers. Anything after
// a "-" or "+" (pre-release, build metadata, `git describe`'s commit suffix) is
// rejected rather than ignored: lazyshell does not publish pre-releases, so a
// tag carrying one is a case this comparison was never designed for.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int

	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" || strings.ContainsAny(v, "-+") {
		return out, false
	}

	fields := strings.Split(v, ".")
	if len(fields) > 3 {
		return out, false
	}

	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return out, false
		}

		out[i] = n
	}

	return out, true
}
