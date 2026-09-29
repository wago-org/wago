//go:build !wago_profile

package profcapture

import "github.com/wago-org/wago"

const profileSidecarAvailable = false

func marshalProfileSidecar(*wago.Compiled, []byte) ([]byte, error) { return nil, nil }

func attachProfileSidecar(*wago.Compiled, *wago.CodeProfile, []byte, []byte) error { return nil }
