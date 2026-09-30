//go:build !linux || tinygo

package profile

import "fmt"

type JITFile struct{ *JITDump }

func OpenJITDump(string) (*JITFile, error) {
	return nil, fmt.Errorf("native jitdump capture requires standard Go on Linux")
}
func (f *JITFile) Close() error { return nil }

func (f *JITFile) RefreshMarker() error { return fmt.Errorf("jitdump discovery requires Linux") }
