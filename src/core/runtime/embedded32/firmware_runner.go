package embedded32

// FirmwareTransportFunction describes one callable export in transport ordinal
// order. Address uses the target's callable representation (including the Thumb
// state bit on Arm32).
type FirmwareTransportFunction struct {
	Address     uint32
	ParamSlots  uint16
	ResultSlots uint16
}

// FirmwareTransportInvoker is the small target-specific boundary that enters
// generated code and restores the static firmware image on instantiation/reset.
// Implementations may use TinyGo or board assembly, but the transport state
// machine itself remains architecture-neutral.
type FirmwareTransportInvoker interface {
	Instantiate(contextAddress uint32) TransportCode
	Start(entryAddress, contextAddress uint32) TransportCode
	Call(entryAddress, contextAddress uint32, parameters, results []uint32) TransportCode
	Cancel(contextAddress uint32) TransportCode
	Reset(contextAddress uint32) TransportCode
}

// FirmwareTransportRunner implements TransportHandler for one prelinked image.
// It is deliberately single-invocation and contains no goroutines or caches.
type FirmwareTransportRunner struct {
	Target         uint32
	MaximumPayload uint32
	ContextAddress uint32
	StartAddress   uint32
	Functions      []FirmwareTransportFunction
	Invoker        FirmwareTransportInvoker
	initialized    bool
	started        bool
}

func (r *FirmwareTransportRunner) Hello() TransportHelloInfo {
	if r == nil {
		return TransportHelloInfo{}
	}
	return TransportHelloInfo{
		Target:          r.Target,
		ContextABIBytes: ContextABISize,
		CallABIBytes:    CallABIBytes,
		MaximumPayload:  r.MaximumPayload,
	}
}

func (r *FirmwareTransportRunner) Instantiate() TransportCode {
	if !r.valid() || r.initialized {
		return TransportCodeState
	}
	code := r.Invoker.Instantiate(r.ContextAddress)
	if code == TransportCodeOK {
		r.initialized = true
		r.started = false
	}
	return code
}

func (r *FirmwareTransportRunner) Start() TransportCode {
	if !r.valid() || !r.initialized || r.started {
		return TransportCodeState
	}
	if r.StartAddress != 0 {
		if code := r.Invoker.Start(r.StartAddress, r.ContextAddress); code != TransportCodeOK {
			return code
		}
	}
	r.started = true
	return TransportCodeOK
}

func (r *FirmwareTransportRunner) Call(exportOrdinal uint32, parameters, results []uint32) TransportCode {
	if !r.valid() || !r.initialized || !r.started || uint64(exportOrdinal) >= uint64(len(r.Functions)) {
		return TransportCodeState
	}
	function := r.Functions[exportOrdinal]
	if function.Address == 0 || uint64(function.ParamSlots) != uint64(len(parameters)) || uint64(function.ResultSlots) != uint64(len(results)) {
		return TransportCodeState
	}
	return r.Invoker.Call(function.Address, r.ContextAddress, parameters, results)
}

func (r *FirmwareTransportRunner) Cancel() TransportCode {
	if !r.valid() || !r.initialized {
		return TransportCodeState
	}
	return r.Invoker.Cancel(r.ContextAddress)
}

func (r *FirmwareTransportRunner) Reset() TransportCode {
	if !r.valid() || !r.initialized {
		return TransportCodeState
	}
	code := r.Invoker.Reset(r.ContextAddress)
	if code == TransportCodeOK {
		r.initialized = false
		r.started = false
	}
	return code
}

func (r *FirmwareTransportRunner) valid() bool {
	return r != nil && r.Invoker != nil && r.Target != 0 && r.MaximumPayload != 0 && r.ContextAddress != 0
}
