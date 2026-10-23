//go:build arm64

package arm64

import (
	"fmt"
)

func (f *fn) setRepresentationLimit(limit functionRepresentationLimit) {
	if f.representationLimit == functionRepresentationOK {
		f.representationLimit = limit
	}
}

func (f *fn) representationError() error {
	var field string
	switch f.representationLimit {
	case functionRepresentationReturnSite:
		field = "return-site branch chain"
	case functionRepresentationFrameEnd:
		field = "forward control-end offset"
	case functionRepresentationCallReloc:
		field = "call relocation"
	case functionRepresentationBranchRange:
		return fmt.Errorf("arm64: native branch displacement exceeds instruction range")
	case functionRepresentationADRRange:
		return fmt.Errorf("arm64: native PC-relative address exceeds instruction range")
	default:
		field = "unknown field"
	}
	return fmt.Errorf("arm64: function %s exceeds compact representation limit", field)
}
