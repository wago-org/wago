package project

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	GlobalEnv = "WAGO_GLOBAL"
	LocalEnv  = "WAGO_LOCAL"
	BareEnv   = "WAGO_BARE"
)

type Scope struct {
	Name         string
	ManifestDir  string
	Dependencies []string
}

func ResolveScope(localDir, globalDir string) (Scope, error) {
	if Truthy(BareEnv) {
		return Scope{Name: "bare"}, nil
	}
	local := Truthy(LocalEnv)
	if local || !Truthy(GlobalEnv) {
		if _, err := os.Stat(Path(localDir)); local || err == nil {
			if local && os.IsNotExist(err) {
				return Scope{}, fmt.Errorf(
					"no local manifest at %s; run 'wago init' or omit --local to use global plugins",
					DisplayPath(localDir),
				)
			}
			dependencies, err := Dependencies(localDir)
			if err != nil {
				return Scope{}, err
			}
			return Scope{Name: "local", ManifestDir: localDir, Dependencies: dependencies}, nil
		}
	}
	dependencies, err := Dependencies(globalDir)
	if err != nil {
		return Scope{}, err
	}
	name := "global"
	if len(dependencies) == 0 {
		name = "plain"
	}
	return Scope{Name: name, ManifestDir: globalDir, Dependencies: dependencies}, nil
}

func SelectScope(global, local, bare bool) error {
	selected := 0
	for _, explicit := range []bool{global, local, bare} {
		if explicit {
			selected++
		}
	}
	if selected > 1 {
		return errors.New("choose only one of --local, --global, or --bare")
	}
	if selected == 0 {
		return nil
	}
	for _, name := range []string{GlobalEnv, LocalEnv, BareEnv} {
		if err := os.Unsetenv(name); err != nil {
			return err
		}
	}
	switch {
	case global:
		return os.Setenv(GlobalEnv, "1")
	case local:
		return os.Setenv(LocalEnv, "1")
	default:
		return os.Setenv(BareEnv, "1")
	}
}

func MutationGlobal(explicitGlobal, explicitLocal bool) (bool, error) {
	if explicitGlobal && explicitLocal {
		return false, errors.New("choose either --global or --local, not both")
	}
	return explicitGlobal, nil
}

func Truthy(name string) bool {
	switch strings.ToLower(os.Getenv(name)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func ScopeLabel(scope Scope) string {
	if scope.Name == "plain" {
		return "global"
	}
	return scope.Name
}
