package wago

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"
)

type mutableInfoExtension struct {
	info ExtensionInfo
}

func (e *mutableInfoExtension) Info() ExtensionInfo    { return e.info }
func (*mutableInfoExtension) Register(*Registry) error { return nil }

func populatedMutableInfo() ExtensionInfo {
	return ExtensionInfo{
		ID: "test.mutable-info", Authors: []string{"author"}, Tags: []string{"tag"},
		Requires: []string{"required"}, Before: []string{"before"}, After: []string{"after"},
		RequiresCapabilities: []PluginCapability{PluginHostImports},
		Compat:               Compatibility{Engines: map[string]string{"wago": "*"}, Platforms: []string{"linux/amd64"}},
	}
}

func mutateExtensionInfo(info *ExtensionInfo, value string) {
	info.Authors[0] = value
	info.Tags[0] = value
	info.Requires[0] = value
	info.Before[0] = value
	info.After[0] = value
	info.RequiresCapabilities[0] = PluginCoreRuntime
	info.Compat.Platforms[0] = value
	info.Compat.Engines["wago"] = value
}

func TestInspectionSnapshotsDeepCopyMutableContainers(t *testing.T) {
	rt := NewRuntime()
	ext := &mutableInfoExtension{info: populatedMutableInfo()}
	if err := rt.Use(ext); err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	baseline, err := json.Marshal(rt.Extensions())
	if err != nil {
		t.Fatal(err)
	}

	mutateExtensionInfo(&ext.info, "extension")
	returned := rt.Extensions()
	mutateExtensionInfo(&returned[0], "caller")
	after, err := json.Marshal(rt.Extensions())
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(baseline) {
		t.Fatalf("extension snapshot mutated:\n got %s\nwant %s", after, baseline)
	}

	mod := callsEnvF(t, rt)
	importBaseline, err := json.Marshal(mod.Imports())
	if err != nil {
		t.Fatal(err)
	}
	imports := mod.Imports()
	imports[0].Params[0] = ValV128
	imports[0].Results[0] = ValV128
	imports[0].ParamTypes[0] = ValueTypeDescriptor{Kind: ValueTypeV128}
	imports[0].ResultTypes[0] = ValueTypeDescriptor{Kind: ValueTypeV128}
	importAfter, err := json.Marshal(mod.Imports())
	if err != nil {
		t.Fatal(err)
	}
	if string(importAfter) != string(importBaseline) {
		t.Fatalf("import snapshot mutated:\n got %s\nwant %s", importAfter, importBaseline)
	}

	metadataBaseline, err := json.Marshal(mod.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	metadata := mod.Metadata()
	metadata.Functions[0].Params[0] = ValV128
	metadata.Functions[0].Exports = append(metadata.Functions[0].Exports, "caller")
	metadataAfter, err := json.Marshal(mod.Metadata())
	if err != nil {
		t.Fatal(err)
	}
	if string(metadataAfter) != string(metadataBaseline) {
		t.Fatalf("module metadata snapshot mutated:\n got %s\nwant %s", metadataAfter, metadataBaseline)
	}
}

func TestInspectionSnapshotsAreRaceFreeAgainstCallerMutation(t *testing.T) {
	rt := NewRuntime()
	ext := &mutableInfoExtension{info: populatedMutableInfo()}
	if err := rt.Use(ext); err != nil {
		t.Fatal(err)
	}
	defer rt.Close()

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			mutateExtensionInfo(&ext.info, "extension")
		}
	}()
	for worker := 0; worker < 2; worker++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				infos := rt.Extensions()
				mutateExtensionInfo(&infos[0], "caller")
			}
		}()
	}
	wg.Wait()
}

func TestInspectionCloneCoverageTracksNestedMutableFields(t *testing.T) {
	assertMutableFieldPaths(t, reflect.TypeOf(ImportSpec{}), []string{
		"Params", "Results", "ParamTypes", "ResultTypes",
	})
	assertMutableFieldPaths(t, reflect.TypeOf(ExtensionInfo{}), []string{
		"Authors", "Tags", "Compat.Engines", "Compat.Platforms", "Requires", "Before", "After", "RequiresCapabilities",
	})

	empty := cloneExtensionInfo(ExtensionInfo{Authors: []string{}, Compat: Compatibility{Engines: map[string]string{}, Platforms: []string{}}})
	if empty.Authors == nil || empty.Compat.Engines == nil || empty.Compat.Platforms == nil {
		t.Fatal("clone collapsed non-nil empty containers")
	}
}

func assertMutableFieldPaths(t *testing.T, typ reflect.Type, want []string) {
	t.Helper()
	var got []string
	var walk func(reflect.Type, string)
	walk = func(current reflect.Type, prefix string) {
		for i := 0; i < current.NumField(); i++ {
			field := current.Field(i)
			path := field.Name
			if prefix != "" {
				path = prefix + "." + path
			}
			switch field.Type.Kind() {
			case reflect.Slice, reflect.Map, reflect.Pointer:
				got = append(got, path)
			case reflect.Struct:
				walk(field.Type, path)
			}
		}
	}
	walk(typ, "")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mutable fields in %s = %v, update clone coverage for %v", typ.Name(), got, want)
	}
}
