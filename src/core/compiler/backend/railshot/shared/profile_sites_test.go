package shared

import (
	"reflect"
	"testing"
)

func TestProfileCompilerSitesFollowRollbackAndCompaction(t *testing.T) {
	var sites CodeSites
	sites.Add(2, 8, "memory-bounds-branch")
	sites.Add(10, 14, "gp-reload")
	sites.Add(14, 18, "gp-reload")
	sites.Add(20, 24, "gp-spill")
	sites.Rewind(20)
	sites.Add(20, 24, "vector-spill")
	mapper, err := NewOffsetMap(28, []DeletedRange{{Off: 4, Len: 4}, {Off: 10, Len: 4}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := RemapNativeCodeSites(sites.Ranges(), &mapper)
	if err != nil {
		t.Fatal(err)
	}
	want := []NativeCodeSite{{Offset: 2, Size: 2, Kind: "memory-bounds-branch"}, {Offset: 6, Size: 4, Kind: "gp-reload"}, {Offset: 12, Size: 4, Kind: "vector-spill"}}
	if !reflect.DeepEqual(got, want) || sites.Ranges()[0].Size != 6 {
		t.Fatal(got, sites.Ranges())
	}
	// Two distinct adjacent operations remain two sites, even with the same kind.
	identity, _ := NewOffsetMap(28, nil)
	got, err = RemapNativeCodeSites(sites.Ranges(), &identity)
	if err != nil || len(got) != 4 {
		t.Fatal(got, err)
	}
}
