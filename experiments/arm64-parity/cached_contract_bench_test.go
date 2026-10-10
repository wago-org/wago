package parity

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	wago "github.com/wago-org/wago"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Focused exact-result cached contracts, including host initialization and memory
// oracles. Vector timing requires an explicitly selected declared vector length.
func BenchmarkCachedContract(b *testing.B) {
	root, id := os.Getenv("WAGO_PARITY_CACHE"), os.Getenv("WAGO_PARITY_CORE_ID")
	if root == "" || id == "" {
		b.Skip("select a cached simple core contract")
	}
	var m struct {
		Workloads []struct {
			ID, Artifact, ABI, Export string
			HostProfile               string `json:"host_profile"`
			Vectors                   *struct {
				InputOffset     uint64 `json:"input_offset"`
				OutputOffset    uint64 `json:"output_offset"`
				InputPtrExport  string `json:"input_ptr_export"`
				OutputPtrExport string `json:"output_ptr_export"`
				OutputLen       uint64 `json:"output_len"`
				Mod             uint64
				Cases           []struct {
					Len uint64
					Out string
				}
			}
			Initialize string
			Input      *struct {
				PointerExport string `json:"pointer_export"`
				Offset        uint64
				Hex           string
			}
			Args   []json.RawMessage
			Oracle struct {
				Kind                string
				Expected            []json.RawMessage
				OutputPointerExport string `json:"output_pointer_export"`
				Memory              []struct {
					Offset uint64
					Hex    string
				}
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		b.Fatal(err)
	}
	if err = json.Unmarshal(data, &m); err != nil {
		b.Fatal(err)
	}
	for _, w := range m.Workloads {
		if w.ID != id {
			continue
		}
		if w.ABI != "core" || w.Oracle.Kind != "exact_u64" && w.Oracle.Kind != "exact_vectors" || w.Oracle.Kind == "exact_vectors" && w.Vectors == nil {
			b.Fatal("requires a dedicated timing harness")
		}
		code, err := os.ReadFile(filepath.Join(root, w.Artifact))
		if err != nil {
			b.Fatal(err)
		}
		args := make([]uint64, len(w.Args))
		for i, v := range w.Args {
			args[i], err = parseCachedUint(v)
			if err != nil {
				b.Fatal(err)
			}
		}
		want := make([]uint64, len(w.Oracle.Expected))
		for i, v := range w.Oracle.Expected {
			want[i], err = parseCachedUint(v)
			if err != nil {
				b.Fatal(err)
			}
		}
		b.Run("Compile", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				c, err := wago.Compile(nil, code)
				if err != nil {
					b.Fatal(err)
				}
				if err = c.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("Exec", func(b *testing.B) {
			c, err := wago.Compile(nil, code)
			if err != nil {
				b.Fatal(err)
			}
			defer c.Close()
			imports := wago.NewImports()
			switch w.HostProfile {
			case "":
			case "assemblyscript-abort-v1":
				imports.HostFunc("env", "abort", func(wago.HostCall) { panic("AssemblyScript abort") }).Params(wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32)
			case "identity-v1":
				imports.HostFunc("wasmbench", "identity", func(call wago.HostCall) { call.SetI32(0, call.I32(0)) }).Params(wago.ValI32).Results(wago.ValI32)
			default:
				b.Fatalf("unsupported host profile %q", w.HostProfile)
			}
			in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
			if err != nil {
				b.Fatal(err)
			}
			defer in.Close()
			pointer := func(name string) uint64 {
				f, e := in.WasmFunc(name)
				if e != nil {
					b.Fatal(e)
				}
				out, e := f.Invoke()
				if e != nil {
					b.Fatal(e)
				}
				if len(out) != 1 {
					b.Fatal("pointer result count")
				}
				return out[0]
			}
			memoryRange := func(offset, size uint64) []byte {
				if in.Memory() == nil {
					b.Fatal("contract requires memory")
				}
				data := in.Memory().UnsafeBytes()
				if offset > uint64(len(data)) || size > uint64(len(data))-offset {
					b.Fatal("memory oracle out of bounds")
				}
				return data[offset : offset+size]
			}
			if w.Initialize != "" {
				init, e := in.WasmFunc(w.Initialize)
				if e != nil {
					b.Fatal(e)
				}
				if _, e = init.Invoke(); e != nil {
					b.Fatal(e)
				}
			}
			if w.Input != nil {
				data, e := hex.DecodeString(w.Input.Hex)
				if e != nil {
					b.Fatal(e)
				}
				offset := w.Input.Offset
				if w.Input.PointerExport != "" {
					base := pointer(w.Input.PointerExport)
					if offset > ^uint64(0)-base {
						b.Fatal("input offset overflow")
					}
					offset += base
				}
				copy(memoryRange(offset, uint64(len(data))), data)
			}
			type span struct {
				offset uint64
				data   []byte
			}
			var spans []span
			if v := w.Vectors; v != nil {
				length, e := strconv.ParseUint(os.Getenv("WAGO_PARITY_VECTOR_LEN"), 10, 64)
				if e != nil {
					b.Fatal("select a declared vector length")
				}
				var selected *struct {
					Len uint64
					Out string
				}
				for i := range v.Cases {
					if v.Cases[i].Len == length {
						selected = &v.Cases[i]
						break
					}
				}
				if selected == nil || v.Mod == 0 {
					b.Fatal("unsupported vector length or pattern")
				}
				input, output := v.InputOffset, v.OutputOffset
				if v.InputPtrExport != "" {
					input = pointer(v.InputPtrExport)
				}
				if v.OutputPtrExport != "" {
					output = pointer(v.OutputPtrExport)
				}
				data := memoryRange(input, length)
				for i := range data {
					data[i] = byte(uint64(i) % v.Mod)
				}
				expected, e := hex.DecodeString(selected.Out)
				if e != nil {
					b.Fatal(e)
				}
				if uint64(len(expected)) != v.OutputLen {
					b.Fatal("vector oracle length")
				}
				args = []uint64{input, length, output}
				want = nil
				spans = append(spans, span{output, expected})
			}
			for _, m := range w.Oracle.Memory {
				data, e := hex.DecodeString(m.Hex)
				if e != nil {
					b.Fatal(e)
				}
				spans = append(spans, span{m.Offset, data})
			}
			checkMemory := func() {
				base := uint64(0)
				if w.Oracle.OutputPointerExport != "" {
					base = pointer(w.Oracle.OutputPointerExport)
				}
				for _, m := range spans {
					if m.offset > ^uint64(0)-base {
						b.Fatal("oracle offset overflow")
					}
					if !bytes.Equal(memoryRange(base+m.offset, uint64(len(m.data))), m.data) {
						b.Fatal("memory oracle mismatch")
					}
				}
			}
			f, err := in.WasmFunc(w.Export)
			if err != nil {
				b.Fatal(err)
			}
			check := func() {
				out, err := f.Invoke(args...)
				if err != nil {
					b.Fatal(err)
				}
				if w.Vectors == nil && len(out) != len(want) {
					b.Fatal("oracle result count")
				}
				for i, v := range want {
					if out[i] != v {
						b.Fatalf("result %d got %d want %d", i, out[i], v)
					}
				}
				checkMemory()
			}
			for i := 0; i < 5; i++ {
				check()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err = f.Invoke(args...); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			check()
		})
		return
	}
	b.Fatalf("cached contract %q not found", id)
}
