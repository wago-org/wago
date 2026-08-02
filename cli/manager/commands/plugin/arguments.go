package plugin

import "strings"

func NormalizeModuleRef(ref string) string {
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

func SplitModuleVersion(spec string) (module, version string) {
	if index := strings.LastIndexByte(spec, '@'); index > 0 {
		return spec[:index], spec[index+1:]
	}
	return spec, ""
}

func SplitVersion(value string) (name, version string) {
	if index := strings.LastIndexByte(value, '@'); index >= 0 {
		return value[:index], value[index+1:]
	}
	return value, ""
}

func SplitCommaList(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func Plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
