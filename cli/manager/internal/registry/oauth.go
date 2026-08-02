package registry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/wago-org/wago/cli/internal/automation"
)

const SuccessHTML = `<!doctype html><html><head><meta charset="utf-8">` +
	`<title>wago — logged in</title></head>` +
	`<body style="font-family:system-ui,sans-serif;text-align:center;padding:4rem">` +
	`<h1>You're logged in ✓</h1><p>You can close this tab and return to your terminal.</p>` +
	`</body></html>`

// PostForm sends a GitHub OAuth form request and decodes its JSON response.
func PostForm(endpoint string, form url.Values, output any) error {
	if err := automation.RequireOnline("authentication request"); err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
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
