package wago

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestCompiledArtifactArchitectureIdentity(t *testing.T) {
	blob, err := (&Compiled{}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var native byte
	switch runtime.GOARCH {
	case "amd64":
		native = 1
	case "arm64":
		native = 2
	default:
		t.Fatalf("missing artifact architecture ID for %s", runtime.GOARCH)
	}
	if got := blob[5] >> 4; got != native {
		t.Fatalf("artifact architecture = %d, want %d for %s", got, native, runtime.GOARCH)
	}
	if got := blob[5] & 0x0f; got != compiledSectionCount {
		t.Fatalf("artifact section count = %d, want %d", got, compiledSectionCount)
	}

	for _, architecture := range []byte{0, (native % 2) + 1, 0x0f} {
		t.Run(fmt.Sprintf("reject-architecture-%d", architecture), func(t *testing.T) {
			foreign := append([]byte(nil), blob...)
			foreign[5] = architecture<<4 | foreign[5]&0x0f
			var direct Compiled
			if err := direct.UnmarshalBinary(foreign); err == nil || !strings.Contains(err.Error(), "architecture") {
				t.Fatalf("UnmarshalBinary foreign architecture error = %v", err)
			}
			if direct.CodeSize() != 0 {
				t.Fatal("UnmarshalBinary retained a foreign native image")
			}

			var streamed Compiled
			n, err := streamed.ReadFrom(bytes.NewReader(foreign))
			if err == nil || !strings.Contains(err.Error(), "architecture") {
				t.Fatalf("ReadFrom foreign architecture error = %v", err)
			}
			if n != 6 {
				t.Fatalf("ReadFrom consumed %d bytes, want only the 6-byte header", n)
			}
			if streamed.CodeSize() != 0 {
				t.Fatal("ReadFrom retained a foreign native image")
			}

			loaded, err := LoadTrustedArtifact(foreign)
			if err == nil || !strings.Contains(err.Error(), "architecture") {
				t.Fatalf("LoadTrustedArtifact foreign architecture error = %v", err)
			}
			if loaded == nil {
				t.Fatal("LoadTrustedArtifact omitted its rejected receiver")
			}
			defer loaded.Close()
			if loaded.CodeSize() != 0 {
				t.Fatal("LoadTrustedArtifact retained a foreign native image")
			}
		})
	}
}
