package wasm

const branchHintSectionName = "metadata.code.branch_hint"

// decodeBranchHintSection decodes the branch-hint code-metadata payload. The
// section name has already been consumed. Target validation needs the code
// section, so it is deliberately performed by validateBranchHints afterwards.
func decodeBranchHintSection(payload []byte) ([]FuncBranchHints, error) {
	r := newReader(payload)
	funcs, err := readVec(r, func(r *reader) (FuncBranchHints, error) {
		funcIndex, err := r.u32()
		if err != nil {
			return FuncBranchHints{}, err
		}
		hints, err := readVec(r, func(r *reader) (BranchHint, error) {
			offset, err := r.u32()
			if err != nil {
				return BranchHint{}, err
			}
			size, err := r.u32()
			if err != nil {
				return BranchHint{}, err
			}
			if size != 1 {
				return BranchHint{}, &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
			}
			direction, err := r.byte()
			if err != nil {
				return BranchHint{}, err
			}
			if direction > 1 {
				return BranchHint{}, &DecodeError{Code: ErrInvalidSection, Offset: r.off() - 1}
			}
			return BranchHint{Offset: offset, Likely: direction == 1}, nil
		})
		if err != nil {
			return FuncBranchHints{}, err
		}
		return FuncBranchHints{FuncIndex: funcIndex, Hints: hints}, nil
	})
	if err != nil {
		return nil, err
	}
	if r.has() {
		return nil, &DecodeError{Code: ErrSectionSizeMismatch, Offset: r.off()}
	}
	for i := 1; i < len(funcs); i++ {
		if funcs[i].FuncIndex <= funcs[i-1].FuncIndex {
			return nil, &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
		}
	}
	for i := range funcs {
		for j := 1; j < len(funcs[i].Hints); j++ {
			if funcs[i].Hints[j].Offset <= funcs[i].Hints[j-1].Offset {
				return nil, &DecodeError{Code: ErrInvalidSection, Offset: r.off()}
			}
		}
	}
	return funcs, nil
}

// validateBranchHints checks that every hint names a defined function and an
// actual if or br_if opcode. The byte-backed decoder has already checked every
// instruction immediate, but this walk gives the metadata its required exact
// instruction-boundary check.
func validateBranchHints(m *Module) error {
	imported := m.ImportedFuncCount()
	for _, funcs := range m.BranchHints {
		if funcs.FuncIndex < uint32(imported) {
			return &DecodeError{Code: ErrInvalidSection}
		}
		localIndex := int(funcs.FuncIndex) - imported
		if localIndex < 0 || localIndex >= len(m.Code) {
			return &DecodeError{Code: ErrInvalidSection}
		}
		fn := m.Code[localIndex]
		r := NewReader(fn.BodyBytes)
		for _, hint := range funcs.Hints {
			want := int(hint.Offset) - int(fn.LocalDeclBytes)
			if want < 0 {
				return &DecodeError{Code: ErrInvalidSection}
			}
			for r.Offset() < want {
				op, err := r.Byte()
				if err != nil {
					return &DecodeError{Code: ErrInvalidSection}
				}
				if err := SkipInstructionImmediate(r, op); err != nil {
					return &DecodeError{Code: ErrInvalidSection}
				}
			}
			if r.Offset() != want {
				return &DecodeError{Code: ErrInvalidSection}
			}
			op, err := r.Byte()
			if err != nil {
				return &DecodeError{Code: ErrInvalidSection}
			}
			if op != 0x04 && op != 0x0d {
				return &DecodeError{Code: ErrInvalidSection}
			}
			if err := SkipInstructionImmediate(r, op); err != nil {
				return &DecodeError{Code: ErrInvalidSection}
			}
		}
	}
	return nil
}
