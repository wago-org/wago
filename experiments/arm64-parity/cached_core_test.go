package parity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	wago "github.com/wago-org/wago"
)

// Optional verification of unchanged cached upstream and call contracts. Each
// case uses a fresh instance, with repeated calls only for stateless contracts.
// This checks exact result/memory/vector oracles, not timing or performance parity.
func TestCachedCoreOracles(t *testing.T) {
	root := os.Getenv("WAGO_PARITY_CACHE")
	if root == "" {
		t.Skip("set WAGO_PARITY_CACHE to the wasm.fyi corpus cache")
	}
	var manifest struct {
		Workloads []struct {
			ID, Artifact, SHA256, ABI string
			HostProfile               string `json:"host_profile"`
			Export                    string
			Initialize                string
			Reset                     string
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
			Args  []json.RawMessage
			Input *struct {
				PointerExport string `json:"pointer_export"`
				Offset        uint64
				Hex           string
			}
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
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, w := range manifest.Workloads {
		if !strings.HasPrefix(w.ID, "wago/") && !strings.HasPrefix(w.ID, "mechanisms/") {
			continue
		}
		t.Run(w.ID, func(t *testing.T) {
			if w.ABI != "core" || w.Oracle.Kind != "exact_u64" && w.Oracle.Kind != "exact_vectors" {
				t.Fatal("unsupported cached contract")
			}
			code, err := os.ReadFile(filepath.Join(root, w.Artifact))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(code)
			if hex.EncodeToString(sum[:]) != w.SHA256 {
				t.Fatal("cached artifact hash mismatch")
			}
			c, err := wago.Compile(nil, code)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if dir := os.Getenv("WAGO_PARITY_CORE_CODE_DIR"); dir != "" {
				if err = os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				file, err := os.Create(filepath.Join(dir, strings.ReplaceAll(w.ID, "/", "__")+".bin"))
				if err != nil {
					t.Fatal(err)
				}
				_, err = c.WriteCodeTo(file)
				file.Close()
				if err != nil {
					t.Fatal(err)
				}
			}

			imports := wago.NewImports()
			switch w.HostProfile {
			case "":
			case "assemblyscript-abort-v1":
				imports.HostFunc("env", "abort", func(wago.HostCall) { panic("AssemblyScript abort") }).Params(wago.ValI32, wago.ValI32, wago.ValI32, wago.ValI32)
			case "identity-v1":
				imports.HostFunc("wasmbench", "identity", func(call wago.HostCall) { call.SetI32(0, call.I32(0)) }).Params(wago.ValI32).Results(wago.ValI32)
			default:
				t.Fatalf("unsupported host profile %q", w.HostProfile)
			}
			in, err := wago.Instantiate(c, wago.InstantiateOptions{Imports: imports})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			pointer := func(name string) uint64 {
				f, err := in.WasmFunc(name)
				if err != nil {
					t.Fatal(err)
				}
				out, err := f.Invoke()
				if err != nil {
					t.Fatal(err)
				}
				if len(out) != 1 {
					t.Fatal("pointer export must return one value")
				}
				return out[0]
			}
			memoryRange := func(offset, size uint64) []byte {
				memory := in.Memory()
				if memory == nil {
					t.Fatal("contract requires memory")
				}
				b := memory.UnsafeBytes()
				if offset > uint64(len(b)) || size > uint64(len(b))-offset {
					t.Fatal("contract memory range out of bounds")
				}
				return b[offset : offset+size]
			}

			if w.Initialize != "" {
				init, err := in.WasmFunc(w.Initialize)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = init.Invoke(); err != nil {
					t.Fatal(err)
				}
			}
			if w.Input != nil {
				input, err := hex.DecodeString(w.Input.Hex)
				if err != nil {
					t.Fatal(err)
				}
				offset := w.Input.Offset
				if w.Input.PointerExport != "" {
					base := pointer(w.Input.PointerExport)
					if offset+base < offset {
						t.Fatal("input offset overflow")
					}
					offset += base
				}
				copy(memoryRange(offset, uint64(len(input))), input)
			}
			args := make([]uint64, len(w.Args))
			for i, s := range w.Args {
				args[i], err = parseCachedUint(s)
				if err != nil {
					t.Fatal(err)
				}
			}
			f, err := in.WasmFunc(w.Export)
			if err != nil {
				t.Fatal(err)
			}

			if w.Vectors != nil {
				v := w.Vectors
				if v.Mod == 0 {
					t.Fatal("zero input pattern modulus")
				}
				input, output := v.InputOffset, v.OutputOffset
				if v.InputPtrExport != "" {
					input = pointer(v.InputPtrExport)
				}
				if v.OutputPtrExport != "" {
					output = pointer(v.OutputPtrExport)
				}
				for _, tc := range v.Cases {
					b := memoryRange(input, tc.Len)
					for i := range b {
						b[i] = byte(uint64(i) % v.Mod)
					}
					if _, err = f.Invoke(input, tc.Len, output); err != nil {
						t.Fatal(err)
					}
					want, err := hex.DecodeString(tc.Out)
					if err != nil {
						t.Fatal(err)
					}
					if uint64(len(want)) != v.OutputLen || !bytes.Equal(memoryRange(output, v.OutputLen), want) {
						t.Fatalf("vector length=%d mismatch", tc.Len)
					}
				}
				return
			}
			if w.Oracle.Kind != "exact_u64" {
				t.Fatal("vector contract lacks vectors")
			}
			calls := 1
			if w.Reset == "stateless" {
				calls = 3
			}
			for call := 0; call < calls; call++ {
				out, err := f.Invoke(args...)
				if err != nil {
					t.Fatal(err)
				}
				if len(out) != len(w.Oracle.Expected) {
					t.Fatal("result count differs from oracle")
				}
				for i, s := range w.Oracle.Expected {
					want, err := parseCachedUint(s)
					if err != nil {
						t.Fatal(err)
					}
					if out[i] != want {
						t.Fatalf("result %d: got %d want %d", i, out[i], want)
					}
				}
				base := uint64(0)
				if w.Oracle.OutputPointerExport != "" {
					base = pointer(w.Oracle.OutputPointerExport)
				}
				for _, span := range w.Oracle.Memory {
					want, err := hex.DecodeString(span.Hex)
					if err != nil {
						t.Fatal(err)
					}
					if base+span.Offset < base {
						t.Fatal("contract memory offset overflow")
					}
					if !bytes.Equal(memoryRange(base+span.Offset, uint64(len(want))), want) {
						t.Fatalf("memory oracle mismatch at %d", span.Offset)
					}
				}
			}
		})
	}
}

// The combined cache mixes string-encoded u64 values with numeric JSON values.
func parseCachedUint(raw json.RawMessage) (uint64, error) {
	s := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, err
		}
	}
	return strconv.ParseUint(s, 10, 64)
}
