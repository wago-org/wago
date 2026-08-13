package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"unicode"

	"github.com/wago-org/wago/cli/internal/automation"
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
	if err := ValidateUniqueJSON(data); err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}

// ValidateUniqueJSON validates one JSON value and rejects object members whose
// names repeat under the case-insensitive matching used by encoding/json.
func ValidateUniqueJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var frames []uniqueJSONFrame
	rootStarted := false
	rootComplete := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if !rootComplete {
				return io.EOF
			}
			return nil
		}
		if err != nil {
			return err
		}
		if rootComplete {
			return errors.New("JSON response contains multiple values")
		}

		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				if err := beginUniqueJSONValue(frames, &rootStarted); err != nil {
					return err
				}
				frames = append(frames, uniqueJSONFrame{object: delimiter == '{', wantKey: delimiter == '{'})
			case '}', ']':
				if len(frames) == 0 {
					return errors.New("JSON response contains an unexpected closing delimiter")
				}
				frame := frames[len(frames)-1]
				if frame.object != (delimiter == '}') || frame.object && !frame.wantKey {
					return errors.New("JSON response contains an invalid closing delimiter")
				}
				frames = frames[:len(frames)-1]
				completeUniqueJSONValue(frames, &rootComplete)
			default:
				return errors.New("JSON response contains an unexpected delimiter")
			}
			continue
		}

		if len(frames) > 0 && frames[len(frames)-1].object && frames[len(frames)-1].wantKey {
			key, ok := token.(string)
			if !ok {
				return errors.New("JSON response contains a non-string object key")
			}
			frame := &frames[len(frames)-1]
			if frame.members == nil {
				frame.members = map[string]struct{}{}
			}
			canonicalKey := foldJSONName(key)
			if _, exists := frame.members[canonicalKey]; exists {
				return errors.New("JSON response contains a duplicate object field")
			}
			frame.members[canonicalKey] = struct{}{}
			frame.wantKey = false
			continue
		}

		if err := beginUniqueJSONValue(frames, &rootStarted); err != nil {
			return err
		}
		completeUniqueJSONValue(frames, &rootComplete)
	}
}

type uniqueJSONFrame struct {
	object  bool
	wantKey bool
	members map[string]struct{}
}

func beginUniqueJSONValue(frames []uniqueJSONFrame, rootStarted *bool) error {
	if len(frames) == 0 {
		if *rootStarted {
			return errors.New("JSON response contains multiple values")
		}
		*rootStarted = true
		return nil
	}
	if frame := frames[len(frames)-1]; frame.object && frame.wantKey {
		return errors.New("JSON response contains an object value without a key")
	}
	return nil
}

func completeUniqueJSONValue(frames []uniqueJSONFrame, rootComplete *bool) {
	if len(frames) == 0 {
		*rootComplete = true
		return
	}
	frame := &frames[len(frames)-1]
	if frame.object {
		frame.wantKey = true
	}
}

func foldJSONName(name string) string {
	return strings.Map(func(character rune) rune {
		for {
			next := unicode.SimpleFold(character)
			if next <= character {
				return next
			}
			character = next
		}
	}, name)
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
