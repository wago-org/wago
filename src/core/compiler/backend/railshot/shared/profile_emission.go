package shared

// EmissionOrigin identifies the Wasm operation driving native emission. Invalid
// origins deliberately leave gaps, including adapters and shared runtime code.
type EmissionOrigin struct {
	Function, PC, InlineParent uint32
	Valid                      bool
}

// SourceEmission records nested lowering scopes without charging a child's
// bytes to its parent. It is compiler-only; all runtime uses are behind the
// profiling build gate. Rewind must accompany tentative native-code rollback.
type SourceEmission struct {
	origin EmissionOrigin
	cursor int
	ranges []NativeSourceRange
}

func (e *SourceEmission) Switch(at int, origin EmissionOrigin) {
	if at < e.cursor {
		e.Rewind(at)
	}
	if at > e.cursor && e.origin.Valid {
		e.ranges = appendSourceRange(e.ranges, NativeSourceRange{Offset: uint64(e.cursor), Size: uint64(at - e.cursor), Function: e.origin.Function, WasmOffset: e.origin.PC, InlineParent: e.origin.InlineParent})
	}
	e.cursor, e.origin = at, origin
}

func (e *SourceEmission) Rewind(at int) {
	if at > e.cursor && e.origin.Valid {
		e.ranges = appendSourceRange(e.ranges, NativeSourceRange{Offset: uint64(e.cursor), Size: uint64(at - e.cursor), Function: e.origin.Function, WasmOffset: e.origin.PC, InlineParent: e.origin.InlineParent})
	}
	for len(e.ranges) > 0 {
		last := &e.ranges[len(e.ranges)-1]
		if last.Offset >= uint64(at) {
			e.ranges = e.ranges[:len(e.ranges)-1]
			continue
		}
		if last.Offset+last.Size > uint64(at) {
			last.Size = uint64(at) - last.Offset
		}
		break
	}
	e.cursor = at
}

// Ranges is borrowed compiler metadata, valid until the next mutation.
func (e *SourceEmission) Ranges() []NativeSourceRange { return e.ranges }

func appendSourceRange(dst []NativeSourceRange, r NativeSourceRange) []NativeSourceRange {
	if r.Size == 0 {
		return dst
	}
	if len(dst) > 0 {
		last := &dst[len(dst)-1]
		if last.Offset+last.Size == r.Offset && last.Function == r.Function && last.WasmOffset == r.WasmOffset && last.InlineParent == r.InlineParent {
			last.Size += r.Size
			return dst
		}
	}
	return append(dst, r)
}

// OverlayNativeSources combines ordered, nonoverlapping compiler directories.
// Specific check origins take precedence over broader lowering ranges. The
// linear merge preserves unknown gaps and does not modify either input.
func OverlayNativeSources(base, checks []NativeSourceRange) []NativeSourceRange {
	out := make([]NativeSourceRange, 0, len(base)+len(checks))
	i := 0
	var current NativeSourceRange
	if len(base) > 0 {
		current = base[0]
	}
	advance := func() {
		i++
		if i < len(base) {
			current = base[i]
		}
	}
	for _, check := range checks {
		for i < len(base) && current.Offset+current.Size <= check.Offset {
			out = appendSourceRange(out, current)
			advance()
		}
		if i < len(base) && current.Offset < check.Offset {
			prefix := current
			prefix.Size = check.Offset - current.Offset
			out = appendSourceRange(out, prefix)
			current.Size -= prefix.Size
			current.Offset = check.Offset
		}
		out = appendSourceRange(out, check)
		end := check.Offset + check.Size
		for i < len(base) && current.Offset+current.Size <= end {
			advance()
		}
		if i < len(base) && current.Offset < end {
			current.Size -= end - current.Offset
			current.Offset = end
		}
	}
	for i < len(base) {
		out = appendSourceRange(out, current)
		advance()
	}
	return out
}
