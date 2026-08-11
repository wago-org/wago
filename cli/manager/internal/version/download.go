package version

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	managerprogress "github.com/wago-org/wago/cli/manager/internal/progress"
	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/httpclient"
	"github.com/wago-org/wago/internal/wagopaths"
)

// releaseAssetLimit is deliberately much larger than current stripped Wago
// executables while still bounding a compromised release endpoint. Downloads
// remain O(copy-buffer) memory and may consume at most this much temporary disk.
const releaseAssetLimit int64 = 512 << 20

var (
	releaseAssetMaximum = releaseAssetLimit
	errChecksumFormat   = errors.New("invalid release checksum format")
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
// verifies its SHA-256 against the sibling ".sha256" file, and atomically
// publishes it to dest with executable permissions.
func downloadBinary(baseURL, ver string, profile wagopaths.Profile, build wagopaths.Build, dest string) error {
	return downloadBinaryContext(context.Background(), baseURL, ver, profile, build, dest)
}

func downloadBinaryContext(ctx context.Context, baseURL, ver string, profile wagopaths.Profile, build wagopaths.Build, dest string) error {
	return downloadBinaryWithProgressContext(ctx, baseURL, ver, profile, build, dest, nil)
}

func downloadBinaryWithProgress(baseURL, ver string, profile wagopaths.Profile, build wagopaths.Build, dest string, progress *managerprogress.Progress) error {
	return downloadBinaryWithProgressContext(context.Background(), baseURL, ver, profile, build, dest, progress)
}

func downloadBinaryWithProgressContext(ctx context.Context, baseURL, ver string, profile wagopaths.Profile, build wagopaths.Build, dest string, progress *managerprogress.Progress) error {
	asset := versionAsset(profile, build)
	return downloadReleaseAssetWithProgressContext(ctx, baseURL, ver, asset, dest, progress)
}

func downloadReleaseAssetWithProgress(baseURL, ver, asset, dest string, progress *managerprogress.Progress) error {
	return downloadReleaseAssetWithProgressContext(context.Background(), baseURL, ver, asset, dest, progress)
}

func downloadReleaseAssetWithProgressContext(ctx context.Context, baseURL, ver, asset, dest string, progress *managerprogress.Progress) error {
	address := fmt.Sprintf("%s/%s/%s", strings.TrimRight(baseURL, "/"), ver, asset)

	if progress != nil {
		progress.Begin("fetching checksum")
	}
	checksumResponse, err := getReleaseBytes(ctx, "release checksum download", address+".sha256", checksumBodyMaximum)
	if err != nil {
		if progress != nil {
			progress.Fail("checksum download failed")
		}
		return fmt.Errorf("fetch checksum: %w", err)
	}
	if checksumResponse.StatusCode != http.StatusOK {
		err := &httpStatusError{url: address + ".sha256", code: checksumResponse.StatusCode, status: checksumResponse.Status}
		if progress != nil {
			if releaseAssetUnavailable(err) {
				progress.Done("checksum unavailable; using source")
			} else {
				progress.Fail("checksum download failed")
			}
		}
		return fmt.Errorf("fetch checksum: %w", err)
	}
	want, err := parseReleaseChecksum(checksumResponse.Body, asset)
	if err != nil {
		if progress != nil {
			progress.Fail("invalid checksum")
		}
		return fmt.Errorf("parse checksum for %s: %w", asset, err)
	}
	if progress != nil {
		progress.Done("fetched checksum")
		progress.Begin("downloading " + asset)
	}

	response, err := openReleaseStream(ctx, "release download", address)
	if err != nil {
		if progress != nil {
			progress.Fail("download failed")
		}
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, readErr := httpclient.ReadBounded(response.Body, response.ContentLength, httpclient.DefaultErrorBodyLimit, address)
		if readErr != nil && !errors.Is(readErr, httpclient.ErrBodyTooLarge) {
			return readErr
		}
		err := &httpStatusError{url: address, code: response.StatusCode, status: response.Status}
		if progress != nil {
			if releaseAssetUnavailable(err) {
				progress.Done("release asset unavailable; using source")
			} else {
				progress.Fail("download failed")
			}
		}
		return err
	}
	if response.ContentLength > releaseAssetMaximum {
		if progress != nil {
			progress.Fail("download exceeds size limit")
		}
		return &httpclient.BodyTooLargeError{URL: address, Limit: releaseAssetMaximum, ContentLength: response.ContentLength}
	}

	err = atomicfile.ReplaceFile(dest, atomicfile.Options{Mode: 0o755, Sync: true}, func(writer io.Writer) error {
		hash := sha256.New()
		output := io.MultiWriter(writer, hash)
		limited := &io.LimitedReader{R: response.Body, N: releaseAssetMaximum + 1}
		buffer := make([]byte, 64<<10)
		var current int64
		for {
			read, readErr := limited.Read(buffer)
			if read > 0 {
				written, writeErr := output.Write(buffer[:read])
				current += int64(written)
				if progress != nil {
					progress.Percent("downloading "+asset, current, response.ContentLength)
				}
				if writeErr != nil {
					return writeErr
				}
				if written != read {
					return io.ErrShortWrite
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return readErr
			}
		}
		if current > releaseAssetMaximum {
			return &httpclient.BodyTooLargeError{URL: address, Limit: releaseAssetMaximum, ContentLength: -1}
		}
		if response.ContentLength >= 0 && current != response.ContentLength {
			return io.ErrUnexpectedEOF
		}
		got := hash.Sum(nil)
		if subtle.ConstantTimeCompare(got, want[:]) != 1 {
			return fmt.Errorf("checksum mismatch for %s (want %x, got %x)", asset, want, got)
		}
		return nil
	})
	if err != nil {
		if progress != nil {
			if errors.Is(err, httpclient.ErrBodyTooLarge) {
				progress.Fail("download exceeds size limit")
			} else if strings.Contains(err.Error(), "checksum mismatch") {
				progress.Fail("checksum verification failed")
			} else {
				progress.Fail("download failed")
			}
		}
		return err
	}
	if progress != nil {
		progress.Done("downloaded and verified " + asset)
		progress.Done("installed executable")
	}
	return nil
}

func parseReleaseChecksum(data []byte, asset string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	line := strings.TrimSuffix(string(data), "\n")
	line = strings.TrimSuffix(line, "\r")
	if line == "" || strings.ContainsAny(line, "\r\n") {
		return digest, errChecksumFormat
	}
	separator := strings.IndexAny(line, " \t")
	if separator != 64 || len(line) <= separator {
		return digest, errChecksumFormat
	}
	digestText := line[:separator]
	remainder := strings.TrimLeft(line[separator:], " \t")
	if strings.HasPrefix(remainder, "*") {
		remainder = remainder[1:]
	}
	if remainder != asset || strings.ContainsAny(remainder, " \t") {
		return digest, errChecksumFormat
	}
	decoded, err := hex.DecodeString(digestText)
	if err != nil || len(decoded) != sha256.Size {
		return digest, errChecksumFormat
	}
	copy(digest[:], decoded)
	return digest, nil
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

// httpGetBytesProgress remains for small metadata tests/callers only. Release
// executables use the dedicated streaming path above.
func httpGetBytesProgress(url string, progress func(current, total int64)) ([]byte, error) {
	response, err := getReleaseBytes(context.Background(), "release metadata download", url, releaseMetadataMaximum)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, &httpStatusError{url: url, code: response.StatusCode, status: response.Status}
	}
	if progress != nil {
		progress(int64(len(response.Body)), int64(len(response.Body)))
	}
	return response.Body, nil
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
