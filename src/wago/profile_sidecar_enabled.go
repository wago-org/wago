//go:build wago_profile

package wago

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/wago-org/wago/internal/jitprofile"
)

const maxCodeProfileSidecarBytes = 64 << 20

// codeProfileSidecar is deliberately separate from the executable artifact.
// Its artifact digest binds every diagnostic range to the exact trusted bytes.
type codeProfileSidecar struct {
	Version        int              `json:"version"`
	ArtifactSHA256 string           `json:"artifact_sha256"`
	CodeSHA256     string           `json:"code_sha256"`
	Target         string           `json:"target"`
	Image          CodeProfileImage `json:"image"`
}

// MarshalCodeProfileSidecar exports optional compiler diagnostics for the exact
// serialized artifact. It requires a wago_profile build and an observed compile.
// The returned JSON contains no native bytes, guest arguments, or guest memory.
func (c *Compiled) MarshalCodeProfileSidecar(artifact []byte) ([]byte, error) {
	if c == nil || !IsCompiled(artifact) {
		return nil, fmt.Errorf("wago: a compiled module and serialized artifact are required")
	}
	c.ensureCodeCache()
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closed && cc.refs == 0 {
		return nil, fmt.Errorf("wago: compiled module is closed")
	}
	p := cc.loadProfile()
	if p == nil || p.image.ModuleID == "" || len(p.image.Regions) == 0 {
		return nil, fmt.Errorf("wago: compilation has no compiler profiling metadata")
	}
	code := c.executionView().code
	if len(code) == 0 {
		return nil, fmt.Errorf("wago: compiled code is unavailable")
	}
	if err := c.executionView().validateSerializableLocked(); err != nil {
		return nil, err
	}
	// Bind diagnostics to the exact serialized module, including its metadata.
	// The caller may have passed another valid artifact with identical code.
	canonical, err := marshalCompiled(c.executionView())
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(artifact, canonical) {
		return nil, fmt.Errorf("wago: artifact does not match profiling metadata")
	}
	artifactHash := sha256.Sum256(artifact)
	codeHash := sha256.Sum256(code)
	image := p.image
	image.Base, image.Size, image.ID = 0, uint64(len(code)), 0
	image.Code = nil
	image.Preexisting = false
	sidecar := codeProfileSidecar{Version: 1, ArtifactSHA256: hex.EncodeToString(artifactHash[:]), CodeSHA256: hex.EncodeToString(codeHash[:]), Target: runtime.GOOS + "/" + runtime.GOARCH, Image: image}
	data, err := json.Marshal(sidecar)
	if len(data) > maxCodeProfileSidecarBytes {
		return nil, fmt.Errorf("wago: profiling sidecar exceeds %d bytes", maxCodeProfileSidecarBytes)
	}
	return data, err
}

// AttachCodeProfileSidecar validates and joins an optional diagnostic sidecar
// to a loaded artifact before instantiation. Mismatch leaves the module usable
// and without guessed attribution. The artifact must already be trusted by the
// caller; a sidecar never changes the trusted loader's execution policy.
func (c *Compiled) AttachCodeProfileSidecar(session *CodeProfile, artifact, sidecar []byte) error {
	if c == nil || session == nil || len(artifact) == 0 || len(sidecar) == 0 {
		return fmt.Errorf("wago: module, session, artifact, and sidecar are required")
	}
	if len(sidecar) > maxCodeProfileSidecarBytes {
		return fmt.Errorf("wago: profiling sidecar exceeds %d bytes", maxCodeProfileSidecarBytes)
	}
	if !IsCompiled(artifact) {
		return fmt.Errorf("wago: profiling sidecar requires a compiled artifact")
	}
	var decoded codeProfileSidecar
	if err := json.Unmarshal(sidecar, &decoded); err != nil {
		return fmt.Errorf("wago: decode profiling sidecar: %w", err)
	}
	if decoded.Version != 1 || decoded.Target != runtime.GOOS+"/"+runtime.GOARCH || decoded.Image.Target != decoded.Target {
		return fmt.Errorf("wago: profiling sidecar version or target mismatch")
	}
	artifactHash := sha256.Sum256(artifact)
	if decoded.ArtifactSHA256 != hex.EncodeToString(artifactHash[:]) {
		return fmt.Errorf("wago: profiling sidecar artifact digest mismatch")
	}
	c.ensureCodeCache()
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closed && cc.refs == 0 {
		return fmt.Errorf("wago: compiled module is closed")
	}
	if cc.loadProfile() != nil {
		return fmt.Errorf("wago: module already has profiling metadata")
	}
	if cc.refs != 0 && session.TraceBoundaries() {
		return fmt.Errorf("wago: attach boundary tracing before instantiation")
	}
	code := c.executionView().code
	codeHash := sha256.Sum256(code)
	if len(code) == 0 || decoded.CodeSHA256 != hex.EncodeToString(codeHash[:]) || decoded.Image.Size != uint64(len(code)) || decoded.Image.ID != 0 || decoded.Image.Base != 0 || len(decoded.Image.Code) != 0 || decoded.Image.ModuleID == "" || decoded.Image.ArtifactID == "" {
		return fmt.Errorf("wago: profiling sidecar code identity mismatch")
	}
	canonical, err := marshalCompiled(c.executionView())
	if err != nil || !bytes.Equal(artifact, canonical) {
		return fmt.Errorf("wago: module does not match profiling sidecar artifact")
	}
	im := decoded.Image
	if err := jitprofile.ValidateRegions(im.Regions, uint64(len(code))); err != nil {
		return fmt.Errorf("wago: profiling sidecar regions: %w", err)
	}
	if err := jitprofile.ValidateSources(im.Sources, uint64(len(code))); err != nil {
		return fmt.Errorf("wago: profiling sidecar sources: %w", err)
	}
	if err := jitprofile.ValidateInlineSources(im.Sources, im.InlineFrames); err != nil {
		return fmt.Errorf("wago: profiling sidecar inline frames: %w", err)
	}
	if err := jitprofile.ValidateCodeSites(im.CodeSites, uint64(len(code))); err != nil {
		return fmt.Errorf("wago: profiling sidecar sites: %w", err)
	}
	if err := jitprofile.ValidateCodeSiteRegions(im.CodeSites, im.Regions); err != nil {
		return fmt.Errorf("wago: profiling sidecar site owners: %w", err)
	}
	if err := jitprofile.ValidateUnwind(im.Unwind, uint64(len(code))); err != nil {
		return fmt.Errorf("wago: profiling sidecar unwind: %w", err)
	}
	cc.attachProfile(&compiledProfile{session: session, image: im, mappings: make(map[uintptr]uint64)}, code)
	return nil
}
