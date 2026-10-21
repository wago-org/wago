//go:build wago_profile

package profcapture

import "github.com/wago-org/wago"

const profileSidecarAvailable = true

func marshalProfileSidecar(c *wago.Compiled, artifact []byte) ([]byte, error) {
	return c.MarshalCodeProfileSidecar(artifact)
}

func attachProfileSidecar(c *wago.Compiled, session *wago.CodeProfile, artifact, sidecar []byte) error {
	return c.AttachCodeProfileSidecar(session, artifact, sidecar)
}
