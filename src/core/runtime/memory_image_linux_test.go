//go:build linux && (amd64 || arm64)

package runtime

import (
	"errors"
	"os"
	"testing"
)

func TestImageMappingUnavailableOnlyForMappingFailure(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "undersized-image-*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, tc := range []struct {
		name  string
		bytes int
		fd    int
	}{
		{name: "invalid size", bytes: -1, fd: int(f.Fd())},
		{name: "bad descriptor", bytes: 65536, fd: -1},
		{name: "undersized image", bytes: 65536, fd: int(f.Fd())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewJobMemoryGrowableFromImage(tc.bytes, 65536, tc.fd)
			if err == nil || errors.Is(err, ErrImageMappingUnavailable) {
				t.Fatalf("non-mapping failure must remain visible: %v", err)
			}
		})
	}
}
