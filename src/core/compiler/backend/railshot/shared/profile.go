package shared

import "github.com/wago-org/wago/internal/jitprofile"

// ProfileFunctionLayout is produced only after all function/module compaction.
type ProfileFunctionLayout struct {
	Entry int
	Index int
	Name  string
	Size  NativeFunctionSizeReport
}

// ProfileRegions uses emitted lengths, never the next entry (which can alias an
// omitted function). Module islands are supplied by their finalizer owner.
func ProfileRegions(functions []ProfileFunctionLayout, islands []jitprofile.Region, codeSize int) []jitprofile.Region {
	var out []jitprofile.Region
	cursor := 0
	add := func(start, size int, kind string, fn int, name string) {
		if size <= 0 {
			return
		}
		out = append(out, jitprofile.Region{Offset: uint64(start), Size: uint64(size), Kind: kind, Function: fn, Name: name})
	}
	for _, f := range functions {
		n := f.Size
		if n.TotalBytes == 0 {
			continue
		}
		add(cursor, f.Entry-cursor, "padding", -1, "")
		pos := f.Entry
		add(pos, n.HostAdapterBytes, "entry-adapter", f.Index, f.Name)
		pos += n.HostAdapterBytes
		add(pos, n.AdapterToInternalPaddingBytes, "padding", -1, "")
		pos += n.AdapterToInternalPaddingBytes
		body := n.InternalFunctionBytes - n.LiteralPoolBytes - n.SharedTrapBodyBytes
		add(pos, body, "guest-body", f.Index, f.Name)
		pos += body
		add(pos, n.SharedTrapBodyBytes, "shared-trap", -1, "")
		pos += n.SharedTrapBodyBytes
		add(pos, n.LiteralPoolBytes, "literal-data", -1, "")
		cursor = f.Entry + n.TotalBytes
	}
	for _, r := range islands {
		add(cursor, int(r.Offset)-cursor, "padding", -1, "")
		out = append(out, r)
		cursor = int(r.Offset + r.Size)
	}
	add(cursor, codeSize-cursor, "padding", -1, "")
	return out
}
