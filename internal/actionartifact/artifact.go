// Package actionartifact downloads checksum-verified executables from GitHub
// Actions artifacts. Callers retain an exact-source fallback because GitHub
// requires authentication for artifact archives and expires them over time.
package actionartifact

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wago-org/wago/internal/atomicfile"
	"github.com/wago-org/wago/internal/httpclient"
)

const (
	metadataLimit int64 = 4 << 20
	archiveLimit  int64 = 512 << 20
	checksumLimit int64 = 4 << 10
)

// Config identifies one repository's Actions artifact catalog. HTTPClient is
// optional and primarily supports deterministic tests.
type Config struct {
	CatalogURL string
	Repository string
	Token      string
	HTTPClient *http.Client
}

type catalog struct {
	Artifacts []artifact `json:"artifacts"`
}

type artifact struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	Expired            bool      `json:"expired"`
	CreatedAt          time.Time `json:"created_at"`
	ArchiveDownloadURL string    `json:"archive_download_url"`
	WorkflowRun        struct {
		ID      int64  `json:"id"`
		HeadSHA string `json:"head_sha"`
	} `json:"workflow_run"`
}

var downloadWithGitHubCLI = githubCLIDownload

// TokenFromEnvironment returns the conventional GitHub API token, when one is
// available. GitHub Actions archive downloads return 401 without one even for
// public repositories; an empty token is still useful because callers must
// attempt the public endpoint before falling back to source.
func TokenFromEnvironment() string {
	for _, name := range []string{"WAGO_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

// DownloadExecutable downloads the newest non-expired host artifact for tag,
// verifies that its workflow commit matches commit (or tag's short SHA), then
// extracts and checksum-verifies asset into destination atomically.
func DownloadExecutable(ctx context.Context, config Config, tag, commit, target, asset, destination string) error {
	if ctx == nil {
		return errors.New("nil Actions artifact context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(config.CatalogURL) == "" {
		return errors.New("Actions artifact catalog URL is empty")
	}
	short, err := canaryShortSHA(tag)
	if err != nil {
		return err
	}
	commit = strings.ToLower(strings.TrimSpace(commit))
	if commit != "" && (!fullCommitSHA(commit) || !strings.HasPrefix(commit, short)) {
		return fmt.Errorf("canary artifact commit %q does not match tag %s", commit, tag)
	}
	artifactName := tag + "-" + target
	selected, err := find(ctx, config, artifactName, commit, short)
	if err != nil {
		return err
	}
	if strings.TrimSpace(config.Repository) != "" {
		directory, tempErr := os.MkdirTemp("", ".wago-gh-artifact-*")
		if tempErr == nil {
			defer os.RemoveAll(directory)
			if ghErr := downloadWithGitHubCLI(ctx, config.Repository, selected.WorkflowRun.ID, selected.Name, directory); ghErr == nil {
				if installErr := installDirectoryExecutable(directory, asset, destination); installErr == nil {
					return nil
				}
			} else if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
		}
	}
	return download(ctx, config, selected, asset, destination)
}

func githubCLIDownload(ctx context.Context, repository string, runID int64, name, directory string) error {
	if runID <= 0 {
		return errors.New("Actions artifact does not identify its workflow run")
	}
	gh, err := exec.LookPath("gh")
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, gh,
		"run", "download", strconv.FormatInt(runID, 10),
		"--repo", repository,
		"--name", name,
		"--dir", directory,
	)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("gh run download: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func find(ctx context.Context, config Config, name, commit, short string) (artifact, error) {
	address, err := url.Parse(config.CatalogURL)
	if err != nil {
		return artifact{}, fmt.Errorf("parse Actions artifact catalog URL: %w", err)
	}
	query := address.Query()
	query.Set("name", name)
	query.Set("per_page", "100")
	address.RawQuery = query.Encode()
	request, err := request(ctx, address.String(), config.Token)
	if err != nil {
		return artifact{}, err
	}
	client := httpclient.New(httpclient.Config{HTTPClient: config.HTTPClient, Timeout: 30 * time.Second})
	response, err := client.Bytes(ctx, request, metadataLimit)
	if err != nil {
		return artifact{}, fmt.Errorf("fetch Actions artifact catalog: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return artifact{}, fmt.Errorf("fetch Actions artifact catalog: GET %s: %s", address, response.Status)
	}
	var items catalog
	if err := json.Unmarshal(response.Body, &items); err != nil {
		return artifact{}, fmt.Errorf("decode Actions artifact catalog: %w", err)
	}
	if len(items.Artifacts) > 100 {
		return artifact{}, errors.New("Actions artifact catalog returned too many artifacts")
	}
	sort.SliceStable(items.Artifacts, func(i, j int) bool {
		if items.Artifacts[i].CreatedAt.Equal(items.Artifacts[j].CreatedAt) {
			return items.Artifacts[i].ID > items.Artifacts[j].ID
		}
		return items.Artifacts[i].CreatedAt.After(items.Artifacts[j].CreatedAt)
	})
	for _, item := range items.Artifacts {
		head := strings.ToLower(strings.TrimSpace(item.WorkflowRun.HeadSHA))
		if item.ID <= 0 || item.Name != name || item.Expired || item.ArchiveDownloadURL == "" || !fullCommitSHA(head) {
			continue
		}
		if commit != "" && head != commit {
			continue
		}
		if commit == "" && !strings.HasPrefix(head, short) {
			continue
		}
		return item, nil
	}
	return artifact{}, fmt.Errorf("no usable Actions artifact named %s", name)
}

func download(ctx context.Context, config Config, item artifact, asset, destination string) error {
	request, err := request(ctx, item.ArchiveDownloadURL, config.Token)
	if err != nil {
		return err
	}
	client := httpclient.New(httpclient.Config{
		HTTPClient:            config.HTTPClient,
		Timeout:               30 * time.Minute,
		ResponseHeaderTimeout: 30 * time.Second,
	})
	response, err := client.Open(ctx, request)
	if err != nil {
		return fmt.Errorf("download Actions artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download Actions artifact: GET %s: %s", item.ArchiveDownloadURL, response.Status)
	}
	if response.ContentLength > archiveLimit {
		return &httpclient.BodyTooLargeError{URL: item.ArchiveDownloadURL, Limit: archiveLimit, ContentLength: response.ContentLength}
	}
	archive, err := os.CreateTemp("", ".wago-actions-artifact-*.zip")
	if err != nil {
		return err
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)
	written, copyErr := io.Copy(archive, io.LimitReader(response.Body, archiveLimit+1))
	closeErr := archive.Close()
	if copyErr != nil {
		return fmt.Errorf("download Actions artifact: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Actions artifact: %w", closeErr)
	}
	if written > archiveLimit {
		return &httpclient.BodyTooLargeError{URL: item.ArchiveDownloadURL, Limit: archiveLimit, ContentLength: -1}
	}
	if response.ContentLength >= 0 && written != response.ContentLength {
		return fmt.Errorf("download Actions artifact: %w", io.ErrUnexpectedEOF)
	}
	return extractExecutable(archivePath, asset, destination)
}

func extractExecutable(archivePath, asset, destination string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open Actions artifact: %w", err)
	}
	defer archive.Close()
	var payload, checksum *zip.File
	for _, file := range archive.File {
		switch file.Name {
		case asset:
			if payload != nil {
				return fmt.Errorf("Actions artifact contains duplicate %s", asset)
			}
			payload = file
		case asset + ".sha256":
			if checksum != nil {
				return fmt.Errorf("Actions artifact contains duplicate %s.sha256", asset)
			}
			checksum = file
		}
	}
	if payload == nil || checksum == nil {
		return fmt.Errorf("Actions artifact does not contain %s and its checksum", asset)
	}
	if payload.UncompressedSize64 > uint64(archiveLimit) {
		return &httpclient.BodyTooLargeError{URL: filepath.Base(archivePath) + ":" + asset, Limit: archiveLimit, ContentLength: int64(payload.UncompressedSize64)}
	}
	want, err := readChecksum(checksum, asset)
	if err != nil {
		return err
	}
	input, err := payload.Open()
	if err != nil {
		return fmt.Errorf("open %s in Actions artifact: %w", asset, err)
	}
	defer input.Close()
	err = atomicfile.ReplaceFile(destination, atomicfile.Options{Mode: 0o755, Sync: true}, func(writer io.Writer) error {
		hash := sha256.New()
		written, err := io.Copy(io.MultiWriter(writer, hash), io.LimitReader(input, archiveLimit+1))
		if err != nil {
			return err
		}
		if written > archiveLimit {
			return &httpclient.BodyTooLargeError{URL: asset, Limit: archiveLimit, ContentLength: -1}
		}
		if uint64(written) != payload.UncompressedSize64 {
			return io.ErrUnexpectedEOF
		}
		if subtle.ConstantTimeCompare(hash.Sum(nil), want[:]) != 1 {
			return fmt.Errorf("checksum mismatch for %s", asset)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("extract %s from Actions artifact: %w", asset, err)
	}
	return nil
}

func installDirectoryExecutable(directory, asset, destination string) error {
	payloadPath := filepath.Join(directory, asset)
	checksumPath := filepath.Join(directory, asset+".sha256")
	payloadInfo, err := os.Lstat(payloadPath)
	if err != nil || !payloadInfo.Mode().IsRegular() || payloadInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("gh artifact does not contain regular file %s", asset)
	}
	if payloadInfo.Size() > archiveLimit {
		return &httpclient.BodyTooLargeError{URL: payloadPath, Limit: archiveLimit, ContentLength: payloadInfo.Size()}
	}
	checksumInfo, err := os.Lstat(checksumPath)
	if err != nil || !checksumInfo.Mode().IsRegular() || checksumInfo.Mode()&os.ModeSymlink != 0 || checksumInfo.Size() > checksumLimit {
		return fmt.Errorf("gh artifact does not contain valid checksum %s.sha256", asset)
	}
	checksum, err := os.ReadFile(checksumPath)
	if err != nil {
		return err
	}
	want, err := parseChecksum(checksum, asset)
	if err != nil {
		return err
	}
	input, err := os.Open(payloadPath)
	if err != nil {
		return err
	}
	defer input.Close()
	err = atomicfile.ReplaceFile(destination, atomicfile.Options{Mode: 0o755, Sync: true}, func(writer io.Writer) error {
		hash := sha256.New()
		written, err := io.Copy(io.MultiWriter(writer, hash), io.LimitReader(input, archiveLimit+1))
		if err != nil {
			return err
		}
		if written > archiveLimit {
			return &httpclient.BodyTooLargeError{URL: payloadPath, Limit: archiveLimit, ContentLength: -1}
		}
		if written != payloadInfo.Size() {
			return io.ErrUnexpectedEOF
		}
		if subtle.ConstantTimeCompare(hash.Sum(nil), want[:]) != 1 {
			return fmt.Errorf("checksum mismatch for %s", asset)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("install %s from gh artifact: %w", asset, err)
	}
	return nil
}

func readChecksum(file *zip.File, asset string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	if file.UncompressedSize64 > uint64(checksumLimit) {
		return digest, errors.New("Actions artifact checksum exceeds size limit")
	}
	reader, err := file.Open()
	if err != nil {
		return digest, fmt.Errorf("open Actions artifact checksum: %w", err)
	}
	defer reader.Close()
	data, err := httpclient.ReadBounded(reader, int64(file.UncompressedSize64), checksumLimit, asset+".sha256")
	if err != nil {
		return digest, err
	}
	return parseChecksum(data, asset)
}

func parseChecksum(data []byte, asset string) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	line := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if line == "" || strings.ContainsAny(line, "\r\n") {
		return digest, errors.New("Actions artifact checksum is malformed")
	}
	separator := strings.IndexAny(line, " \t")
	if separator != 64 {
		return digest, errors.New("Actions artifact checksum is malformed")
	}
	name := strings.TrimLeft(line[separator:], " \t")
	name = strings.TrimPrefix(name, "*")
	if name != asset && name != "./"+asset {
		return digest, errors.New("Actions artifact checksum names the wrong file")
	}
	decoded, err := hex.DecodeString(line[:separator])
	if err != nil || len(decoded) != sha256.Size {
		return digest, errors.New("Actions artifact checksum is malformed")
	}
	copy(digest[:], decoded)
	return digest, nil
}

func request(ctx context.Context, address, token string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "wago-cli")
	if token = strings.TrimSpace(token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request, nil
}

func canaryShortSHA(tag string) (string, error) {
	marker := "-canary.g"
	index := strings.LastIndex(strings.ToLower(strings.TrimSpace(tag)), marker)
	if index < 0 {
		return "", fmt.Errorf("%q is not a canary tag", tag)
	}
	short := strings.ToLower(strings.TrimSpace(tag))[index+len(marker):]
	if len(short) != 7 {
		return "", fmt.Errorf("%q is not a canary tag", tag)
	}
	for _, char := range short {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return "", fmt.Errorf("%q is not a canary tag", tag)
		}
	}
	return short, nil
}

func fullCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, char := range value {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}
