package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
	"github.com/wago-org/wago/internal/jsonstrict"
)

var oauthResponseMaximum int64 = 128 << 10

// PostForm sends a GitHub OAuth form request and decodes its bounded JSON response.
func PostForm(endpoint string, form url.Values, output any) error {
	return PostFormContext(context.Background(), endpoint, form, output)
}

func PostFormContext(ctx context.Context, endpoint string, form url.Values, output any) error {
	if err := automation.RequireOnline("authentication request"); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := registryHTTP.Bytes(ctx, request, oauthResponseMaximum)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OAuth endpoint returned status %d", response.StatusCode)
	}
	return unmarshalUniqueJSON(response.Body, output)
}

// unmarshalUniqueJSON rejects duplicate object members before decoding. OAuth
// responses contain security decisions whose meaning must not depend on whether
// a decoder keeps the first or last spelling of a repeated field.
func unmarshalUniqueJSON(data []byte, output any) error {
	if err := ValidateUniqueFoldedJSON(data); err != nil {
		return err
	}
	if err := json.Unmarshal(data, output); err != nil {
		return errors.New("JSON response does not match the expected schema")
	}
	return nil
}

// ValidateUniqueJSON rejects exactly repeated JSON object keys.
func ValidateUniqueJSON(data []byte) error { return jsonstrict.ValidateUniqueJSON(data) }

// ValidateUniqueFoldedJSON also rejects case-folded keys outside exact subtrees.
func ValidateUniqueFoldedJSON(data []byte, exactSubtrees ...string) error {
	return jsonstrict.ValidateUniqueFoldedJSON(data, exactSubtrees...)
}

func OpenBrowser(target string) error {
	if err := automation.RequireOnline("browser authentication"); err != nil {
		return err
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}

// CopyClipboard writes short user-facing text to the platform clipboard.
func CopyClipboard(text string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("pbcopy")
	case "windows":
		command = exec.Command("clip.exe")
	default:
		for _, candidate := range []struct {
			name string
			args []string
		}{
			{name: "wl-copy"},
			{name: "xclip", args: []string{"-selection", "clipboard"}},
			{name: "xsel", args: []string{"--clipboard", "--input"}},
		} {
			if _, err := exec.LookPath(candidate.name); err == nil {
				command = exec.Command(candidate.name, candidate.args...)
				break
			}
		}
		if command == nil {
			return errors.New("no clipboard command found")
		}
	}
	command.Stdin = strings.NewReader(text)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}
