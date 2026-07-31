package version

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/wagopaths"
)

type httpStatusError struct {
	url    string
	code   int
	status string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("GET %s: %s", e.url, e.status)
}

func releaseAssetUnavailable(err error) bool {
	var statusErr *httpStatusError
	return errors.As(err, &statusErr) && statusErr.code == http.StatusNotFound
}

// downloadBinary fetches the host-platform wago binary for ver from baseURL,
// verifies its SHA-256 against the sibling ".sha256" file, and writes it to dest
// (0755). It writes nothing on a checksum mismatch.
func downloadBinary(baseURL, ver string, profile wagopaths.Profile, build wagopaths.Build, dest string) error {
	return downloadBinaryWithProgress(baseURL, ver, profile, build, dest, nil)
}

func downloadBinaryWithProgress(baseURL, ver string, profile wagopaths.Profile, build wagopaths.Build, dest string, progress *managerprogress.Progress) error {
	asset := versionAsset(profile, build)
	return downloadReleaseAssetWithProgress(baseURL, ver, asset, dest, progress)
}

func downloadReleaseAssetWithProgress(baseURL, ver, asset, dest string, progress *managerprogress.Progress) error {
	url := fmt.Sprintf("%s/%s/%s", strings.TrimRight(baseURL, "/"), ver, asset)

	if progress != nil {
		progress.Begin("downloading " + asset)
	}
	body, err := httpGetBytesProgress(url, func(current, total int64) {
		if progress != nil {
			progress.Percent("downloading "+asset, current, total)
		}
	})
	if err != nil {
		if progress != nil {
			if releaseAssetUnavailable(err) {
				progress.Done("release asset unavailable; using source")
			} else {
				progress.Fail("download failed")
			}
		}
		return err
	}
	if progress != nil {
		progress.Done("downloaded " + asset)
		progress.Begin("fetching checksum")
	}
	sumRaw, err := httpGetBytes(url + ".sha256")
	if err != nil {
		if progress != nil {
			if releaseAssetUnavailable(err) {
				progress.Done("checksum unavailable; using source")
			} else {
				progress.Fail("checksum download failed")
			}
		}
		return fmt.Errorf("fetch checksum: %w", err)
	}
	if progress != nil {
		progress.Done("fetched checksum")
		progress.Begin("verifying SHA-256")
	}
	want := strings.TrimSpace(string(sumRaw))
	if fields := strings.Fields(want); len(fields) > 0 {
		want = fields[0] // accept "<hash>  <filename>" form
	}
	got := sha256.Sum256(body)
	if !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		if progress != nil {
			progress.Fail("checksum verification failed")
		}
		return fmt.Errorf("checksum mismatch for %s (want %s, got %x)", asset, want, got)
	}
	if progress != nil {
		progress.Done("verified SHA-256")
		progress.Begin("installing executable")
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, body, 0o755); err != nil {
		if progress != nil {
			progress.Fail("could not write executable")
		}
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		if progress != nil {
			progress.Fail("could not install executable")
		}
		return err
	}
	if progress != nil {
		progress.Done("installed executable")
	}
	return nil
}

func versionAsset(profile wagopaths.Profile, build wagopaths.Build) string {
	return "wago-runtime-" + string(profile) + "-" + string(build) + "-" + runtime.GOOS + "-" + runtime.GOARCH
}

func managerAsset() string {
	return "wago-" + runtime.GOOS + "-" + runtime.GOARCH
}

func httpGetBytes(url string) ([]byte, error) {
	return httpGetBytesProgress(url, nil)
}

func httpGetBytesProgress(url string, progress func(current, total int64)) ([]byte, error) {
	if err := automation.RequireOnline("release download"); err != nil {
		return nil, err
	}
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{url: url, code: resp.StatusCode, status: resp.Status}
	}
	var body bytes.Buffer
	buf := make([]byte, 32*1024)
	var current int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			_, _ = body.Write(buf[:n])
			current += int64(n)
			if progress != nil {
				progress(current, resp.ContentLength)
			}
		}
		if readErr == io.EOF {
			return body.Bytes(), nil
		}
		if readErr != nil {
			return nil, readErr
		}
	}
}

// releaseBase is the base URL for release binary assets, overridable for testing.
func releaseBase() string {
	if v := os.Getenv("WAGO_RELEASE_BASE"); v != "" {
		return v
	}
	return "https://github.com/wago-org/wago/releases/download"
}

// releaseAPI is the GitHub API base, overridable for testing.
func releaseAPI() string {
	if v := os.Getenv("WAGO_RELEASE_API"); v != "" {
		return v
	}
	return "https://api.github.com"
}
