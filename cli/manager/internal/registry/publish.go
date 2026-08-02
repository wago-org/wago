package registry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func registryPublish(options PublishRequest) {
	manifestPath := options.Manifest
	var ver string
	commit := options.Commit
	notes := options.Notes
	category := options.Category
	tags := options.Tags
	token := resolveToken()
	if token == "" {
		fatal("publish: not logged in (run: wago auth login)")
	}
	if manifestPath == "" {
		// The standard manifest is wago.json; fall back to the older
		// wago-plugin.json name if that's what the module ships.
		manifestPath = "wago.json"
		for _, cand := range []string{"wago.json", "wago-plugin.json"} {
			if _, err := os.Stat(cand); err == nil {
				manifestPath = cand
				break
			}
		}
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		fatal("publish: reading manifest: %v", err)
	}
	// wago.json is self-similar: a subpackage may be a "./path/wago.json" string.
	// The server can't read those local files, so inline them here before upload.
	raw, err = InlineManifest(raw, filepath.Dir(manifestPath))
	if err != nil {
		fatal("publish: resolving subpackage refs in %s: %v", manifestPath, err)
	}
	var mf struct {
		Module  string `json:"module"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &mf); err != nil {
		fatal("publish: parsing %s: %v", manifestPath, err)
	}
	if strings.TrimSpace(mf.Module) == "" {
		fatal("publish: %s has no \"module\" field", manifestPath)
	}

	// The version comes from the manifest, falling back to the newest git tag.
	ver = strings.TrimSpace(mf.Version)
	if ver == "" {
		ver = strings.TrimSpace(GitOutput("describe", "--tags", "--abbrev=0"))
	}
	if ver == "" {
		fatal("publish: no version — set \"version\" in %s or tag the repo", manifestPath)
	}
	if commit == "" {
		commit = strings.TrimSpace(GitOutput("rev-parse", "HEAD")) // best-effort; "" if not a repo
	}

	body := map[string]any{
		"manifest": json.RawMessage(raw),
		"version":  ver,
		"commit":   commit,
		"notes":    notes,
		"category": category,
	}
	if t := splitCommaList(tags); len(t) > 0 {
		body["tags"] = t
	}
	if kb := UnpackedKB(filepath.Dir(manifestPath)); kb > 0 {
		body["unpackedKB"] = kb
	}

	status, data, err := apiRequest(http.MethodPost, "/api/publish", token, body)
	if err != nil {
		fatal("publish: %v", err)
	}
	switch status {
	case http.StatusOK:
		fmt.Printf("%s Published %s %s\n", cyan("✓"), bold(mf.Module), ver)
		fmt.Printf("  %s\n", dim(packageURL(mf.Module)))
	case http.StatusConflict:
		fatal("publish: version %s is already published", ver)
	case http.StatusForbidden:
		fatal("publish: you are not the owner of %s", mf.Module)
	case http.StatusUnauthorized:
		fatal("publish: not logged in (run: wago auth login)")
	default:
		fatal("publish: %s", apiError(status, data))
	}
}

// registryUnpublish removes a whole package, or a single version when the
// argument carries an @version suffix. It confirms first unless --yes is given.
func registryUnpublish(options UnpublishRequest) {
	yes := options.Yes
	token := resolveToken()
	if token == "" {
		fatal("unpublish: not logged in (run: wago auth login)")
	}
	name, ver := splitVersion(options.Target)
	target := name
	if ver != "" {
		target = name + "@" + ver
	}
	if !yes && !confirm(fmt.Sprintf("Unpublish %s? This cannot be undone.", target)) {
		fmt.Println("aborted")
		return
	}

	path := "/api/packages/" + url.PathEscape(name)
	if ver != "" {
		path += "/versions/" + url.PathEscape(ver)
	}
	status, data, err := apiRequest(http.MethodDelete, path, token, nil)
	if err != nil {
		fatal("unpublish: %v", err)
	}
	switch status {
	case http.StatusOK:
		fmt.Printf("%s Unpublished %s\n", cyan("✓"), target)
	case http.StatusForbidden:
		fatal("unpublish: you are not the owner of %s", name)
	case http.StatusNotFound:
		fatal("unpublish: %s not found", target)
	case http.StatusUnauthorized:
		fatal("unpublish: not logged in (run: wago auth login)")
	default:
		fatal("unpublish: %s", apiError(status, data))
	}
}

// registryDeprecate marks a package (or a specific @version) deprecated, or
// reverses it with --undo. --message sets the deprecation notice.
func registryDeprecate(options DeprecateRequest) {
	undo := options.Undo
	message := options.Message
	token := resolveToken()
	if token == "" {
		fatal("deprecate: not logged in (run: wago auth login)")
	}
	name, ver := splitVersion(options.Target)
	target := name
	if ver != "" {
		target = name + "@" + ver
	}

	body := map[string]any{"message": message, "version": ver, "undo": undo}
	path := "/api/packages/" + url.PathEscape(name) + "/deprecate"
	status, data, err := apiRequest(http.MethodPost, path, token, body)
	if err != nil {
		fatal("deprecate: %v", err)
	}
	switch status {
	case http.StatusOK:
		if undo {
			fmt.Printf("%s Un-deprecated %s\n", cyan("✓"), target)
		} else {
			fmt.Printf("%s Deprecated %s\n", cyan("✓"), target)
		}
	case http.StatusForbidden:
		fatal("deprecate: you are not the owner of %s", name)
	case http.StatusNotFound:
		fatal("deprecate: %s not found", target)
	case http.StatusUnauthorized:
		fatal("deprecate: not logged in (run: wago auth login)")
	default:
		fatal("deprecate: %s", apiError(status, data))
	}
}

// confirm prompts on stderr and reads a yes/no answer from stdin (default no).
func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.TrimSpace(strings.ToLower(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
