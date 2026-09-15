package railssa

import "fmt"

type ProofKind uint8

const (
	ProofInvalid ProofKind = iota
	ProofNonZero
	ProofUpper32Zero
	ProofBounds
)

type ProofRequest struct {
	Value FlowValueID
	Block BlockID
	Aux   uint32
	Fuel  uint16
	Kind  ProofKind
	_     uint8
}

type ProofResult struct {
	Certificate  uint32
	Dependencies HeapMask
	Steps        uint16
	Proven       bool
	_            uint8
}

type proofCacheEntry struct {
	request ProofRequest
	result  ProofResult
}

type ProofEngine struct {
	Flow     *ValueFlow
	Semantic *SemanticFunc
	Metadata *Metadata
	Facts    *SimplifyResult

	cache []proofCacheEntry
}

func NewProofEngine(flow *ValueFlow, semantic *SemanticFunc, metadata *Metadata, facts *SimplifyResult) (*ProofEngine, error) {
	if flow == nil || semantic == nil || metadata == nil || !facts.HasIntegerFactDomain(len(flow.Values)) {
		return nil, fmt.Errorf("railssa: proof engine requires verified semantic facts")
	}
	return &ProofEngine{Flow: flow, Semantic: semantic, Metadata: metadata, Facts: facts}, nil
}

// DemandProof answers only an explicit bounded query. Failed and successful
// answers are cached by kind, value, scope, auxiliary property, and fuel.
func (e *ProofEngine) DemandProof(request ProofRequest) (ProofResult, error) {
	if e == nil {
		return ProofResult{}, fmt.Errorf("railssa: invalid proof request %#v", request)
	}
	for _, entry := range e.cache {
		if entry.request == request {
			return entry.result, nil
		}
	}
	result, err := e.demandProofOnce(request)
	if err != nil {
		return ProofResult{}, err
	}
	return e.cacheResult(request, result), nil
}

// demandProofOnce is the allocation-free query path for internal forward
// passes that prove they issue every request exactly once.
func (e ProofEngine) demandProofOnce(request ProofRequest) (ProofResult, error) {
	if request.Kind == ProofInvalid || request.Fuel == 0 || request.Value == 0 || int(request.Value) >= len(e.Flow.Values) {
		return ProofResult{}, fmt.Errorf("railssa: invalid proof request %#v", request)
	}
	result := ProofResult{}
	value := request.Value
	for result.Steps < request.Fuel {
		result.Steps++
		alias := e.Facts.Aliases[value]
		if alias == value {
			break
		}
		value = alias
	}
	if e.Facts.Aliases[value] != value {
		return result, nil
	}
	fact := e.Facts.IntegerFactAt(value)
	switch request.Kind {
	case ProofNonZero:
		result.Proven = fact.Known && fact.Min != 0
	case ProofUpper32Zero:
		result.Proven = fact.Width == 32 || fact.KnownZero&0xffffffff00000000 == 0xffffffff00000000
	case ProofBounds:
		if int(request.Aux) >= len(e.Semantic.Insts) {
			return ProofResult{}, fmt.Errorf("railssa: bounds proof instruction %d is unavailable", request.Aux)
		}
		for id, certificate := range e.Facts.Bounds {
			if certificate.Instruction == request.Aux && resolveAlias(e.Facts.Aliases, certificate.Address) == value {
				result.Proven = true
				result.Certificate = uint32(id) + 1
				result.Dependencies = HeapLinearMemory
				break
			}
		}
	default:
		return ProofResult{}, fmt.Errorf("railssa: unsupported proof kind %d", request.Kind)
	}
	if err := e.VerifyProof(request, result); err != nil {
		return ProofResult{}, err
	}
	return result, nil
}

func (e *ProofEngine) cacheResult(request ProofRequest, result ProofResult) ProofResult {
	const maxProofCacheEntries = 1024
	if len(e.cache) < maxProofCacheEntries {
		e.cache = append(e.cache, proofCacheEntry{request: request, result: result})
	}
	return result
}

func (e *ProofEngine) VerifyProof(request ProofRequest, result ProofResult) error {
	if result.Steps > request.Fuel {
		return fmt.Errorf("railssa: proof used %d steps with fuel %d", result.Steps, request.Fuel)
	}
	if !result.Proven {
		if result.Certificate != 0 || result.Dependencies != 0 {
			return fmt.Errorf("railssa: failed proof carries evidence")
		}
		return nil
	}
	value := resolveAlias(e.Facts.Aliases, request.Value)
	fact := e.Facts.IntegerFactAt(value)
	switch request.Kind {
	case ProofNonZero:
		if !fact.Known || fact.Min == 0 {
			return fmt.Errorf("railssa: nonzero proof has no supporting fact")
		}
	case ProofUpper32Zero:
		if fact.Width != 32 && fact.KnownZero&0xffffffff00000000 != 0xffffffff00000000 {
			return fmt.Errorf("railssa: upper-zero proof has no supporting known bits")
		}
	case ProofBounds:
		if result.Certificate == 0 || int(result.Certificate) > len(e.Facts.Bounds) || result.Dependencies != HeapLinearMemory {
			return fmt.Errorf("railssa: bounds proof has invalid certificate")
		}
		certificate := e.Facts.Bounds[result.Certificate-1]
		if certificate.Instruction != request.Aux || resolveAlias(e.Facts.Aliases, certificate.Address) != value || certificate.End > certificate.MemoryBytes {
			return fmt.Errorf("railssa: bounds proof certificate does not match request")
		}
	}
	return nil
}
