package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
)

// GenerateSchema replaces the registered settings name enums
// without reformatting the hand-authored manifest schema around them.
func GenerateSchema(source []byte) ([]byte, error) {
	if !json.Valid(source) {
		return nil, fmt.Errorf("invalid manifest schema JSON")
	}
	result := append([]byte(nil), source...)
	names := SchemaNames()
	for _, section := range []string{"features", "optimizations", "experimental"} {
		pattern := regexp.MustCompile(`(?s)("` + section + `"\s*:\s*\{.*?"propertyNames"\s*:\s*\{\s*"enum"\s*:\s*)\[[^]]*\]`)
		match := pattern.FindSubmatchIndex(result)
		if match == nil {
			return nil, fmt.Errorf("schema settings.%s enum not found", section)
		}
		var rendered bytes.Buffer
		rendered.WriteString("[\n")
		for index, name := range names[section] {
			fmt.Fprintf(&rendered, "              %q", name)
			if index+1 != len(names[section]) {
				rendered.WriteByte(',')
			}
			rendered.WriteByte('\n')
		}
		rendered.WriteString("            ]")
		result = append(append(append([]byte(nil), result[:match[3]]...), rendered.Bytes()...), result[match[1]:]...)
	}
	return result, nil
}
