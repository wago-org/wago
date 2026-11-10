//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"encoding/hex"
	"testing"
)

const (
	gcReferenceResultDirectCallHex       = "0061736d010000000113045e78015e63000160037f7f7f016400600000030403020303060a016401004108fb07010b070801046d61696e00020801010c01010a4d033c020163000164004100044020002001200212000b23002000fb0b012203d14504402003d40f0b20012002fb0900002104230020002004fb0e0120040b0b0041074100410410001a0b02000b0b0701010461626364005c046e616d650110020006737472696e670105737461727402290100050005696e64657801066f666673657402066c656e6774680306636163686564040576616c7565040e020005627974657301047265667307080100056361636865"
	gcReferenceResultRootAcrossCallHex   = "0061736d010000000110035e780160037f7f7f0164006000017f0303020102070801046d61696e00010c01010a320217004100044020002001200212000b20012002fb0900000b1800410041004104100041014104410410001a4100fb0d000b0b0b010108616263645758595a0059046e616d65010d0200046d616b6501046d61696e021a0100030005696e64657801066f666673657402066c656e677468041e030005627974657301096d616b652d7479706502096d61696e2d74797065090701000464617461"
	functionReferenceResultDirectCallHex = "0061736d01000000010d036000017f600001640060000003050400010202070801046d61696e0003080102090501030001000a23040400412a0b0b004100044012010bd2000b0d0010011400412a470440000b0b02000b0036046e616d6501150300046c65616601056d616b65720205737461727404180200096c6561662d74797065010a6d616b65722d74797065"
)

func instantiateCore3Fixture(t *testing.T, encoded string) {
	t.Helper()
	data, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	instance, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
}

func TestGCReferenceResultDirectCallUsesRegisterABI(t *testing.T) {
	instantiateCore3Fixture(t, gcReferenceResultDirectCallHex)
}

func TestGCReferenceResultDirectCallPreservesResultRoot(t *testing.T) {
	data, err := hex.DecodeString(gcReferenceResultRootAcrossCallHex)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	profiles := []struct {
		name string
		gc   GCConfig
	}{
		{name: "throughput", gc: GCConfig{Profile: GCProfileThroughput, StressNurseryBytes: 64, CollectEveryAlloc: true, ForceMajorEveryMinor: true, VerifyAfterCollect: true, ThroughputHeapBytes: 4096, ThroughputPageBytes: 4096}},
		{name: "tiny", gc: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 64, TinyBlockBytes: 16, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}},
	}
	for _, profile := range profiles {
		t.Run(profile.name, func(t *testing.T) {
			instance, err := instantiateCore(compiled, InstantiateOptions{GC: profile.gc})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			got, err := instance.Invoke("main")
			if err != nil || len(got) != 1 || got[0] != 'a' {
				t.Fatalf("main = %v, %v; want [%d]", got, err, 'a')
			}
		})
	}
}

func TestFunctionReferenceResultDirectCallUsesRegisterABI(t *testing.T) {
	instantiateCore3Fixture(t, functionReferenceResultDirectCallHex)
}
