package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// PrintJSON emits stable, indented JSON without HTML escaping.
func PrintJSON(value any) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		Fatal("json: %v", err)
	}
	fmt.Print(buffer.String())
}
