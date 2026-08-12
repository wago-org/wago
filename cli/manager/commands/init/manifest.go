package initcmd

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/src/core/semver"
)

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

func pluginManifest(values answers, existing map[string]any) (map[string]any, int, error) {
	values.name = strings.TrimSpace(defaultValue(values.name, inferDefaults(existing).name))
	if values.name == "" || len(values.name) > 100 {
		return nil, 0, fmt.Errorf("name must contain 1 to 100 characters")
	}
	values.description = strings.TrimSpace(values.description)
	if values.description == "" || len(values.description) > 500 {
		return nil, 0, fmt.Errorf("description must contain 1 to 500 characters")
	}
	plugins, err := parsePlugins(values.plugins, existing["plugins"])
	if err != nil {
		return nil, 0, err
	}
	fields := map[string]any{"plugins": plugins}
	values.module = strings.TrimSpace(values.module)
	if !validModule(values.module) {
		return nil, 0, fmt.Errorf("plugin packages need a valid --module such as github.com/acme/wago-plugin")
	}
	values.version = strings.TrimSpace(defaultValue(values.version, "0.0.0"))
	if _, err := semver.Parse(values.version); err != nil {
		return nil, 0, fmt.Errorf("version %q is not semantic", values.version)
	}
	values.license = strings.TrimSpace(values.license)
	if values.license == "" {
		return nil, 0, fmt.Errorf("plugin packages need an SPDX license")
	}
	values.repository = strings.TrimSpace(defaultValue(values.repository, repositoryForModule(values.module)))
	if !validHTTPSURL(values.repository) {
		return nil, 0, fmt.Errorf("plugin packages need a public HTTPS repository")
	}
	stability := strings.ToLower(strings.TrimSpace(defaultValue(values.stability, "experimental")))
	if stability != "experimental" && stability != "stable" && stability != "deprecated" {
		return nil, 0, fmt.Errorf("stability must be experimental, stable, or deprecated")
	}
	pkg := map[string]any{
		"module": values.module, "version": values.version, "name": values.name, "description": values.description,
		"license": values.license, "repository": values.repository, "stability": stability,
		"engines": map[string]any{"wago": "*"},
	}
	setOptional(pkg, "homepage", values.homepage)
	if category := strings.ToLower(strings.TrimSpace(values.category)); category != "" {
		if !slugPattern.MatchString(category) || len(category) > 64 {
			return nil, 0, fmt.Errorf("category must be a lowercase slug")
		}
		pkg["category"] = category
	}
	if tags := commaList(values.tags); len(tags) > 0 {
		for _, tag := range tags {
			if !slugPattern.MatchString(tag) || len(tag) > 40 {
				return nil, 0, fmt.Errorf("tag %q must be a lowercase slug", tag)
			}
		}
		pkg["tags"] = tags
	}
	if author := strings.TrimSpace(values.author); author != "" {
		pkg["authors"] = []any{map[string]any{"name": author}}
	} else {
		return nil, 0, fmt.Errorf("plugin packages need at least one author")
	}
	fields["package"] = pkg
	return fields, len(plugins), nil
}

func parsePlugins(spec string, current any) (map[string]any, error) {
	plugins := map[string]any{}
	if existing, ok := current.(map[string]any); ok {
		for id, constraint := range existing {
			plugins[id] = constraint
		}
	}
	for _, item := range commaList(spec) {
		id, constraint := item, "*"
		if at := strings.LastIndexByte(item, '@'); at > 0 {
			id, constraint = item[:at], item[at+1:]
		}
		id = strings.TrimSpace(id)
		constraint = strings.TrimSpace(constraint)
		if err := project.ValidatePluginID(id); err != nil {
			return nil, err
		}
		if err := project.ValidateConstraint(constraint); err != nil {
			return nil, fmt.Errorf("plugin %q has invalid version constraint %q", id, constraint)
		}
		plugins[id] = constraint
	}
	return plugins, nil
}

func commaList(value string) []string {
	seen := map[string]bool{}
	var values []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			values = append(values, item)
		}
	}
	return values
}

func setOptional(fields map[string]any, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		fields[key] = value
	}
}

func validModule(module string) bool {
	return project.ValidatePluginID(module) == nil
}

func validHTTPSURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func repositoryForModule(module string) string {
	if strings.HasPrefix(module, "github.com/") {
		parts := strings.Split(module, "/")
		if len(parts) >= 3 {
			return "https://github.com/" + parts[1] + "/" + parts[2]
		}
	}
	return ""
}

func moduleFromGoMod() string {
	file, err := os.Open("go.mod")
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func manifestString(manifest map[string]any, key, fallback string) string {
	if value, ok := manifest[key].(string); ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func firstAuthor(value any) string {
	items, _ := value.([]any)
	if len(items) > 0 {
		if item, ok := items[0].(map[string]any); ok {
			name, _ := item["name"].(string)
			return name
		}
	}
	return ""
}

func joinedStrings(value any) string {
	items, _ := value.([]any)
	values := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			values = append(values, text)
		}
	}
	return strings.Join(values, ", ")
}

func joinedPlugins(value any) string {
	plugins, _ := value.(map[string]any)
	ids := make([]string, 0, len(plugins))
	for id := range plugins {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	values := make([]string, 0, len(ids))
	for _, id := range ids {
		constraint, _ := plugins[id].(string)
		values = append(values, id+"@"+constraint)
	}
	return strings.Join(values, ", ")
}
