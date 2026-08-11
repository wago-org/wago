package registry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
)

var oauthResponseMaximum int64 = 1 << 20

const SuccessHTML = `<!doctype html><html><head><meta charset="utf-8">` +
	`<title>wago — logged in</title></head>` +
	`<body style="font-family:system-ui,sans-serif;text-align:center;padding:4rem">` +
	`<h1>You're logged in ✓</h1><p>You can close this tab and return to your terminal.</p>` +
	`</body></html>`

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
		return fmt.Errorf("OAuth endpoint returned %s", response.Status)
	}
	return json.Unmarshal(response.Body, output)
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

func RandomState() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}
