package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectSchemaCoversEveryLocalSetting(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				PropertyNames struct {
					Enum []string `json:"enum"`
				} `json:"propertyNames"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	settingsSchema, ok := schema.Defs["settings"]
	if !ok {
		t.Fatal("schema.json has no local settings definition")
	}
	known := map[string]bool{}
	for section, property := range settingsSchema.Properties {
		for _, name := range property.PropertyNames.Enum {
			known[section+"."+name] = true
		}
	}
	for _, setting := range allKnownBoolean() {
		if !known[setting.Key] {
			t.Errorf("schema.json omits %s", setting.Key)
		}
		delete(known, setting.Key)
	}
	for key := range known {
		t.Errorf("schema.json contains unregistered setting %s", key)
	}
}
