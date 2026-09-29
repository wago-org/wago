package wago

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/wago-org/wago/internal/jitprofile"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

const codeProfileEnabled = jitprofile.Enabled

// CodeProfileOptions controls bounded, opt-in diagnostic retention. Native bytes
// are copied only with IncludeCode. No guest memory or argument values are saved.
type CodeProfileOptions = jitprofile.Options

// CodeProfile records mapping loads and retirements, independently of sampling.
// Only modules compiled with WithCodeProfile or explicitly attached are observed.
type CodeProfile = codeProfileSession

// CodeProfileImage describes one executable mapping generation.
type CodeProfileImage = jitprofile.Image

// CodeProfileSourceRange maps final native bytes to recorded Wasm provenance.
type CodeProfileSourceRange = jitprofile.SourceRange

// CodeProfileSite identifies an emitted operation, never a dynamic count.
type CodeProfileSite = jitprofile.CodeSite

// CodeProfileUnwindRange records sparse stack recovery rules for final native bytes.
// Missing ranges and unspecified registers are unknown. It does not enable sampling.
type CodeProfileUnwindRange = jitprofile.UnwindRange

// CodeProfileInlineFrame records a static inline caller location.
type CodeProfileInlineFrame = jitprofile.InlineFrame

// CodeProfileRegion is an exact half-open range in final native code.
type CodeProfileRegion = jitprofile.Region

// CodeProfileFunction contains static compiler counters, not executed counts.
type CodeProfileFunction = jitprofile.Function

// CodeProfileEvent is an ordered mapping lifecycle event.
type CodeProfileEvent = jitprofile.Event

// CodeProfileSpan describes elapsed boundary time, never sampled CPU time.
type CodeProfileSpan = jitprofile.Span

// CodeProfileSpanToken owns a reserved boundary record, not runtime execution.
type CodeProfileSpanToken = codeProfileToken

// CodeProfileStatus reports capture loss and the timestamp clock.
type CodeProfileStatus = jitprofile.Status

// NewCodeProfile creates a bounded metadata journal. It does not start a sampler.
// Without wago_profile it returns a closed, empty compatibility session;
// RuntimeConfig.Validate rejects attaching it to execution.
func NewCodeProfile(options CodeProfileOptions) *CodeProfile { return newCodeProfile(options) }

// WithCodeProfile requires a build with -tags=wago_profile and enables code-neutral finalized regions and compiler statistics.
// The session must outlive capture. Profiling metadata is not serialized in .wago
// artifacts; reloaded artifacts report unknown regions until recompiled.
func (c *RuntimeConfig) WithCodeProfile(session *CodeProfile) *RuntimeConfig {
	n := *c
	n.codeProfile = session
	return &n
}

type compiledProfile struct {
	session  *CodeProfile
	image    CodeProfileImage
	mu       sync.Mutex
	mappings map[uintptr]uint64
}

func (p *compiledProfile) register(base uintptr, code []byte, regions []CodeProfileRegion, functions []CodeProfileFunction, bodyImage, preexisting bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.mappings[base]; ok {
		return
	}
	im := p.image
	im.Preexisting = preexisting
	if !bodyImage {
		im.CodeSites = nil
		im.SiteCoverage = ""
		im.Unwind = nil
		im.UnwindCoverage = ""
		im.Sources = nil
		im.InlineFrames = nil
		im.SourceCoverage = ""
	}
	im.Base = uint64(base)
	im.Size = uint64(len(code))
	im.Regions = regions
	im.Functions = functions
	p.mappings[base] = p.session.Register(im, code)
}
func (p *compiledProfile) retire(base uintptr) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.session.Retire(p.mappings[base])
	delete(p.mappings, base)
}

func (c *Compiled) registerProfileCodeLocked() {
	if !jitprofile.Enabled {
		return
	}
	if p := c.codeCache.loadProfile(); p != nil {
		p.register(c.codeCache.base, c.code, p.image.Regions, p.image.Functions, true, false)
	}
}

// unmapProfileCode is called only after executable ownership is released. Retire
// before releasing the address to the OS so a concurrent address reuse cannot
// overtake the old generation's retirement. No guest can execute in this interval.
func (c *Compiled) unmapProfileCode(mem []byte) error {
	if len(mem) == 0 {
		return nil
	}
	if cc := c.loadCodeCache(); jitprofile.Enabled && cc != nil {
		cc.retireProfileImage(uintptr(unsafe.Pointer(&mem[0])))
	}
	return coreruntime.Unmap(mem)
}

// AttachCodeProfile observes current executable mappings and subsequent mapping
// events. Attachment and teardown are ordered, and retained native bytes are
// copied before unmapping. Compiler/source metadata cannot be reconstructed from
// an unobserved compilation or artifact; the body then has one unknown region.
// Attach before instantiation to capture initialization. Boundary tracing cannot
// be enabled midway through existing instances' execution.
func (c *Compiled) AttachCodeProfile(session *CodeProfile) error {
	if !jitprofile.Enabled {
		return fmt.Errorf("wago: profiling requires a build with -tags=wago_profile")
	}
	if c == nil || session == nil {
		return fmt.Errorf("wago: profiling requires a module and session")
	}
	c.ensureCodeCache()
	cc := c.codeCache
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if cc.closed && cc.refs == 0 {
		return fmt.Errorf("wago: compiled module is closed")
	}
	if p := cc.loadProfile(); p != nil {
		if p.session != session {
			return fmt.Errorf("wago: module already belongs to another code profile")
		}
		return nil
	}
	if cc.refs != 0 && session.TraceBoundaries() {
		return fmt.Errorf("wago: attach boundary tracing before instantiation")
	}
	code := c.code
	if len(code) == 0 {
		if memo := c.loadValidateMemo(); memo != nil {
			if snapshot := memo.executionView(); snapshot != nil {
				code = snapshot.code
			}
		}
	}
	if len(code) == 0 {
		return fmt.Errorf("wago: compiled code is unavailable")
	}
	hash := sha256.Sum256(code)
	p := &compiledProfile{session: session, mappings: make(map[uintptr]uint64), image: CodeProfileImage{
		ArtifactID: hex.EncodeToString(hash[:]), Target: runtime.GOOS + "/" + runtime.GOARCH,
		Regions: []CodeProfileRegion{{Offset: 0, Size: uint64(len(code)), Kind: "unknown", Function: -1}},
	}}
	cc.attachProfile(p, code)
	return nil
}

func (c *Compiled) registerProfileThunks(base uintptr, blob []byte, offsets map[uint32]int) {
	if !jitprofile.Enabled {
		return
	}
	// Exact emitted thunk boundaries are captured by the builder, before mapping.
	// Every thunk in this blob was appended in import-index order.
	regions := make([]CodeProfileRegion, 0, len(offsets))
	for i := 0; i < c.NumImports; i++ {
		if off, ok := offsets[uint32(i)]; ok {
			if len(regions) > 0 {
				r := &regions[len(regions)-1]
				r.Size = uint64(off) - r.Offset
			}
			regions = append(regions, CodeProfileRegion{Offset: uint64(off), Size: uint64(len(blob) - off), Kind: "host-thunk", Function: i, Name: c.Imports[i]})
		}
	}
	c.codeCache.registerProfileThunk(base, blob, regions)
}
