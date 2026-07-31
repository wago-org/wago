package plugin

import (
	"fmt"
	"strings"
)

type Package struct {
	Module    string
	Requested string
	Resolved  string
	Exact     string
}

type Resolver func(string) (string, error)

func normalizeModuleRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ref
	}
	path, version := ref, ""
	if index := strings.IndexByte(ref, '@'); index >= 0 {
		path, version = ref[:index], ref[index:]
	}
	if index := strings.IndexByte(path, '/'); index > 0 {
		if first := path[:index]; !strings.Contains(first, ".") {
			path = "github.com/" + path
		}
	}
	return path + version
}

func splitModuleVersion(spec string) (module, version string) {
	if index := strings.LastIndexByte(spec, '@'); index > 0 {
		return spec[:index], spec[index+1:]
	}
	return spec, ""
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func ResolvePackages(specs []string, resolve Resolver) ([]Package, error) {
	packages := make([]Package, 0, len(specs))
	seen := make(map[string]string, len(specs))
	for _, spec := range specs {
		module, version := splitModuleVersion(normalizeModuleRef(spec))
		if module == "" {
			return nil, fmt.Errorf("empty package name")
		}
		if !strings.Contains(module, "/") && !strings.Contains(module, ".") {
			resolved, err := resolve(module)
			if err != nil {
				return nil, fmt.Errorf("%v (or pass the full module path)", err)
			}
			module = resolved
		}
		if previous, exists := seen[module]; exists {
			if previous == version {
				continue
			}
			return nil, fmt.Errorf("package %s requested more than once with conflicting versions", module)
		}
		seen[module] = version
		packages = append(packages, Package{Module: module, Requested: version})
	}
	if len(packages) == 0 {
		return nil, fmt.Errorf("need at least one package")
	}
	return packages, nil
}
