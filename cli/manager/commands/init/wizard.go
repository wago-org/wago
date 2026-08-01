package initcmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/wago-org/wago/cli/internal/command"
	"github.com/wago-org/wago/cli/internal/project"
	"github.com/wago-org/wago/cli/internal/tui"
	"github.com/wago-org/wago/cli/internal/ui"
)

const (
	modeRun    = "run"
	modePlugin = "plugin"
)

type result struct {
	Mode      string
	Created   bool
	Cancelled bool
	Plugins   int
}

type answers struct {
	name, description, plugins                     string
	module, version, license, repository, homepage string
	category, tags, author, stability              string
}

var errCancelled = errors.New("cancelled")

func run(ctx *command.Ctx, in io.Reader, out io.Writer, interactive bool) (result, error) {
	mode, err := explicitMode(ctx)
	if err != nil {
		return result{}, err
	}
	if mode == "" {
		if !interactive {
			mode = modeRun
		} else {
			selected, ok := chooseSetupMode()
			if !ok {
				return result{Cancelled: true}, nil
			}
			mode = selected
		}
	}

	fields := map[string]any{}
	pluginCount := 0
	if mode == modePlugin {
		manifest, err := project.Read(".")
		if err != nil {
			return result{}, err
		}
		values := answersFromContext(ctx)
		if interactive && !ctx.Bool("yes") {
			values, err = askPluginSetup(in, out, values, manifest)
			if errors.Is(err, errCancelled) {
				return result{Cancelled: true}, nil
			}
			if err != nil {
				return result{}, err
			}
		}
		fields, pluginCount, err = pluginManifest(values, manifest)
		if err != nil {
			return result{}, err
		}
	}

	created, err := project.InitializeWith(".", fields)
	if err != nil {
		return result{}, err
	}
	project.EnsureGitignore(".wago/")
	return result{Mode: mode, Created: created, Plugins: pluginCount}, nil
}

func explicitMode(ctx *command.Ctx) (string, error) {
	if ctx.Bool("run") && ctx.Bool("plugin") {
		return "", errors.New("choose --run or --plugin")
	}
	configured := hasPluginAnswers(ctx)
	if ctx.Bool("run") && configured {
		return "", errors.New("--run cannot be combined with plugin setup options")
	}
	switch {
	case ctx.Bool("plugin") || configured:
		return modePlugin, nil
	case ctx.Bool("run"):
		return modeRun, nil
	}
	return "", nil
}

func hasPluginAnswers(ctx *command.Ctx) bool {
	for _, name := range []string{
		"name", "description", "plugins", "module", "version", "license",
		"repository", "homepage", "category", "tags", "author", "stability",
	} {
		if ctx.Str(name) != "" {
			return true
		}
	}
	return false
}

func answersFromContext(ctx *command.Ctx) answers {
	return answers{
		name: ctx.Str("name"), description: ctx.Str("description"), plugins: ctx.Str("plugins"),
		module: ctx.Str("module"), version: ctx.Str("version"), license: ctx.Str("license"), repository: ctx.Str("repository"),
		homepage: ctx.Str("homepage"), category: ctx.Str("category"), tags: ctx.Str("tags"), author: ctx.Str("author"),
		stability: ctx.Str("stability"),
	}
}

func setupModePicker() *tui.Picker {
	return tui.NewPicker("What would you like to do?", []tui.Item{
		{Label: "Run WebAssembly", Value: modeRun, Description: "Create a minimal Wago project"},
		{Label: "Set up a plugin", Value: modePlugin, Description: "Configure a publishable Wago plugin"},
	})
}

func stabilityPicker() *tui.Picker {
	return tui.NewPicker("How stable is this package?", []tui.Item{
		{Label: "Experimental", Value: "experimental", Description: "API may change"},
		{Label: "Stable", Value: "stable", Description: "Ready for production use"},
		{Label: "Deprecated", Value: "deprecated", Description: "Kept for existing users"},
	})
}

func chooseSetupMode() (string, bool) { return choose(setupModePicker()) }
func chooseStability() (string, bool) { return choose(stabilityPicker()) }

func choose(picker *tui.Picker) (string, bool) {
	submitted, cancelled := tui.Run(picker)
	if !submitted || cancelled {
		return "", false
	}
	return picker.Selected(), true
}

func askPluginSetup(in io.Reader, out io.Writer, values answers, manifest map[string]any) (answers, error) {
	reader := bufio.NewReader(in)
	defaults := inferDefaults(manifest)
	values.name = ask(reader, out, "Project name", values.name, defaults.name)
	values.description = ask(reader, out, "Description", values.description, defaults.description)
	values.plugins = ask(reader, out, "Initial plugins (comma-separated)", values.plugins, defaults.plugins)
	values.module = ask(reader, out, "Go module", values.module, defaults.module)
	values.version = ask(reader, out, "Version", values.version, defaultValue(defaults.version, "0.0.0"))
	values.license = ask(reader, out, "License (SPDX)", values.license, defaults.license)
	values.repository = ask(reader, out, "Repository", values.repository, defaultValue(defaults.repository, repositoryForModule(values.module)))
	values.homepage = ask(reader, out, "Homepage", values.homepage, defaultValue(defaults.homepage, values.repository))
	values.category = ask(reader, out, "Category", values.category, defaults.category)
	values.tags = ask(reader, out, "Tags (comma-separated)", values.tags, defaults.tags)
	values.author = ask(reader, out, "Author", values.author, defaults.author)
	if values.stability == "" {
		selected, ok := chooseStability()
		if !ok {
			return answers{}, errCancelled
		}
		values.stability = selected
	}
	return values, nil
}

func ask(reader *bufio.Reader, out io.Writer, label, provided, fallback string) string {
	if strings.TrimSpace(provided) != "" {
		return strings.TrimSpace(provided)
	}
	fmt.Fprintln(out, ui.Bold(label))
	if fallback != "" {
		fmt.Fprintf(out, "› %s ", ui.Dim("["+fallback+"]"))
	} else {
		fmt.Fprint(out, "› ")
	}
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return fallback
	}
	return line
}

func inferDefaults(manifest map[string]any) answers {
	cwd, _ := os.Getwd()
	defaults := answers{name: filepath.Base(cwd)}
	defaults.name = manifestString(manifest, "name", defaults.name)
	defaults.description = manifestString(manifest, "description", "")
	defaults.module = manifestString(manifest, "module", moduleFromGoMod())
	defaults.version = manifestString(manifest, "version", "")
	defaults.license = manifestString(manifest, "license", "")
	defaults.repository = manifestString(manifest, "repository", repositoryForModule(defaults.module))
	defaults.homepage = manifestString(manifest, "homepage", defaults.repository)
	defaults.category = manifestString(manifest, "category", "")
	defaults.stability = manifestString(manifest, "stability", "experimental")
	defaults.author = firstString(manifest["authors"])
	defaults.tags = joinedStrings(manifest["tags"])
	defaults.plugins = joinedPlugins(manifest["plugins"])
	return defaults
}

func defaultValue(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
