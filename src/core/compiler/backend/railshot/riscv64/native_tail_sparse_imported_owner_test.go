//go:build linux && (amd64 || arm64 || riscv64) && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

type sparseImportedFixture struct {
	owner      *NativeTailOwner
	build      func() (*NativeTailOwner, error)
	signatures []*wasm.CompType
	imports    []NativeTailImport
	entries    []int
	leases     []*tailPublishedOwnerForTest
	counts     NativeTailOwnerCounts
	guest      int
}

// Every case contains a certified native import. host adds a shared or owned
// scalar callback with a distinct identity to the same physical owner.
func sparseImportedOwnerFixture(t *testing.T, holes []bool, host, owned bool, types []wasm.ValType) *sparseImportedFixture {
	t.Helper()
	sig := wasm.CompType{Kind: wasm.CompFunc, Params: types, Results: append([]wasm.ValType(nil), types...)}
	locals := make([]tailRAFunction, len(holes))
	entries := make([]int, len(holes))
	var code []byte
	for i := range locals {
		locals[i] = tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
		for len(code)&15 != 0 {
			code = append(code, 0x13, 0, 0, 0)
		}
		entries[i] = len(code)
		code = append(code, locals[i].snapshot...)
	}
	buffer := tailRAProofBuffer(t, code)
	for i := range locals {
		f := &locals[i]
		f.code, f.snapshot, f.info.Entry = buffer, code, entries[i]
		end := len(code)
		if i+1 < len(entries) {
			end = entries[i+1]
		}
		f.info.CodeBytes = end - entries[i]
		f.job, f.context = locals[0].job, locals[0].context
	}
	remote := tailRAProofFunctionFixture(t, &sig, abi.RV64InstanceContextBytes)
	binary.LittleEndian.PutUint64(remote.directory[16:], uint64(remote.job.LinMemBase()))
	proxy, proof := tailDynamicProxyForTest(t, locals[0], remote, 0)
	functions := []tailRAFunction{proxy}
	var hostCode []byte
	var hostCertificate *NativeTailHostCertificate
	if host {
		var err error
		hostCode, hostCertificate, err = NativeTailHostThunkWithCertificate(37, len(types), len(types), owned)
		if err != nil {
			t.Fatal(err)
		}
		functions = append(functions, tailRAFunction{code: tailRAProofBuffer(t, hostCode), snapshot: hostCode, signature: &sig, job: locals[0].job, context: locals[0].context, directory: locals[0].directory, info: FunctionInfo{CodeBytes: len(hostCode)}})
	}
	ni := len(functions)
	functions = append(functions, locals...)
	o := tailDynamicOwnerForTest(t, functions)
	targets := append([]tailRAFunction(nil), o.functions...)
	targets[0] = remote
	proxies := make([]*tailRADynamicProxy, len(functions))
	proxies[0] = proof
	tailDynamicProxyRowsForTest(&o, targets, proxies)
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		t.Fatal(err)
	}
	callee := NativeImportTarget{Entry: remote.codeBase(), Home: remote.job.LinMemBase(), Context: tailRAPointer(remote.context), Arguments: proof.arguments, Results: proof.results, ArgumentSlots: swarSignatureSlots(types), ResultSlots: swarSignatureSlots(types), StackOwnedActivation: true, InvocationContext: true, HostTrampoline: trampoline}
	bridge, certificate, err := NativeImportThunkWithCertificate(callee, locals[0].job.LinMemBase(), tailRAPointer(o.context), 0)
	if err != nil || !bytes.Equal(bridge, proxy.snapshot) {
		t.Fatal("native bridge provenance", err)
	}
	source, sourceLease := tailPublishedFunctionForTest(t, o.functions[ni])
	proxy, proxyLease := tailPublishedFunctionForTest(t, o.functions[0])
	remote, remoteLease := tailPublishedFunctionForTest(t, remote)
	trueTarget, err := NewNativeTailFunction(remote.published, remote.job, remote.context, remote.directory, 0, len(remote.snapshot), 0, sig)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &sparseImportedFixture{entries: entries, guest: len(code) + len(bridge) + len(remote.snapshot), leases: []*tailPublishedOwnerForTest{sourceLease, proxyLease, remoteLease}}
	fixture.imports = []NativeTailImport{{Image: proxy.published, CodeBytes: len(bridge), Target: trueTarget, Arguments: proof.arguments, Results: proof.results, Certificate: certificate}}
	if host {
		h, lease := tailPublishedFunctionForTest(t, o.functions[1])
		binary.LittleEndian.PutUint64(o.directory[abi.RV64TailTargetsHeaderBytes+abi.RV64TailTargetsRowBytes+24:], abi.RV64TailTargetsHost)
		identity, err := NewNativeTailHostFunction(h.published, hostCertificate, source.job, o.context, o.descriptors, 0, 37, owned, sig)
		if err != nil {
			t.Fatal(err)
		}
		fixture.imports = append(fixture.imports, NativeTailImport{Image: h.published, CodeBytes: len(hostCode), Host: identity})
		fixture.leases = append(fixture.leases, lease)
		fixture.guest += len(hostCode)
	}
	n, nonCallable := len(functions), 0
	fixture.signatures = make([]*wasm.CompType, n)
	for i := range fixture.signatures {
		clone := sig
		fixture.signatures[i] = &clone
		if i >= ni && holes[i-ni] {
			nonCallable++
			fixture.signatures[i] = nil
			clear(o.descriptors[40*(i+1) : 40*(i+2)])
			clear(o.directory[32+32*i : 32+32*(i+1)])
		}
	}
	fixture.counts = NativeTailOwnerCounts{Owners: 1, Functions: n, Values: 2 * len(types) * (n - nonCallable), Targets: n, TargetValues: 2 * len(types) * (n - nonCallable), ProxySlots: n, Proxies: 1, MaskSlots: n, NonCallable: nonCallable, TargetNonCallable: nonCallable}
	if host {
		fixture.counts.HostSlots = n
	}
	fixture.build = func() (*NativeTailOwner, error) {
		return NewNativeTailSparseImportedOwner(source.published, source.job, o.context, o.descriptors, o.directory, fixture.entries, fixture.signatures, fixture.imports, trampoline)
	}
	fixture.owner, err = fixture.build()
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestNativeTailSparseImportedOwnerPhysicalSpansAndBudgets(t *testing.T) {
	for _, holes := range [][]bool{{true, false, false}, {false, true, false}, {false, false, true}, {true, true, true}, {false, false, false}} {
		for mode := range 3 {
			t.Run(fmt.Sprintf("holes%v-mode%d", holes, mode), func(t *testing.T) {
				f := sparseImportedOwnerFixture(t, holes, mode != 0, mode == 2, []wasm.ValType{wasm.I32, wasm.F64})
				ni := len(f.imports)
				for local, hole := range holes {
					i := ni + local
					a, b := f.owner.owner.functions[i], f.owner.owner.targets[i]
					if a.info.Entry != f.entries[local] || a.info.mode.functionIndex != uint32(i) || (a.signature == nil) != hole || f.owner.owner.uncallable[i] != hole || !sameNativeTailPhysicalFunction(a, b) {
						t.Fatal("physical source/target ordinal", i)
					}
				}
				for _, sites := range []int{1, 3} {
					m, c, err := NativeTailFactoryCountsBudget(sites, 4*sites, 2, f.counts, f.guest)
					am, ac, actualErr := NativeTailDefinedFactoryBudget(sites, 4*sites, 2, []*NativeTailOwner{f.owner}, f.guest)
					if err != nil || actualErr != nil || m != am || c != ac {
						t.Fatal("sparse imported count-only/frozen budget", m, am, c, ac, err, actualErr)
					}
				}
				if a := testing.AllocsPerRun(4, func() {
					if f.owner.Validate() != nil {
						panic("warm sparse imported validation")
					}
				}); a != 0 {
					t.Fatal("warm sparse imported allocation", a)
				}
				// The factory independently snapshots masks and callable signatures.
				snapshot := (&tailRADynamicSite{owners: []tailRADynamicOwner{f.owner.owner}}).freeze()
				f.owner.owner.uncallable[ni] = !f.owner.owner.uncallable[ni]
				if snapshot.owners[0].uncallable[ni] != holes[0] || snapshot.owners[0].validate(f.owner.trampoline) != nil {
					t.Fatal("frozen factory mask")
				}
				f.owner.owner.uncallable[ni] = holes[0]
				f.signatures[0] = nil
				if bad, err := f.build(); bad != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("nil imported signature accepted", err)
				}
				if err := f.owner.Validate(); err != nil {
					t.Fatal("input mutation changed retained owner", err)
				}
			})
		}
	}
}

func TestNativeTailSparseImportedOwnerRejectsCallableHoles(t *testing.T) {
	f := sparseImportedOwnerFixture(t, []bool{true, false, true}, true, true, []wasm.ValType{wasm.I32})
	for _, i := range []int{2, 4} {
		for _, row := range [][]byte{f.owner.owner.descriptors[40*(i+1) : 40*(i+2)], f.owner.owner.directory[32+32*i : 32+32*(i+1)]} {
			for off := range row {
				row[off] = 1
				if err := f.owner.Validate(); !errors.Is(err, ErrUnsupported) {
					t.Fatal("nonzero internal row", i, off, err)
				}
				if bad, err := f.build(); bad != nil || !errors.Is(err, ErrUnsupported) {
					t.Fatal("constructor admitted callable internal row", i, off, err)
				}
				row[off] = 0
			}
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*tailRADynamicOwner)
	}{
		{"mask-short", func(o *tailRADynamicOwner) { o.uncallable = o.uncallable[:4] }},
		{"mask-missing", func(o *tailRADynamicOwner) { o.uncallable = nil }},
		{"mask-flipped", func(o *tailRADynamicOwner) { o.uncallable[2] = false }},
		{"target-short", func(o *tailRADynamicOwner) { o.targets = o.targets[:4] }},
		{"key", func(o *tailRADynamicOwner) { o.signatureKeys[2] = 1 }},
		{"source-signature", func(o *tailRADynamicOwner) { o.functions[2].signature = o.functions[0].signature }},
		{"target-signature", func(o *tailRADynamicOwner) { o.targets[2].signature = o.targets[0].signature }},
		{"target-image", func(o *tailRADynamicOwner) { o.targets[2].published = o.targets[0].published }},
		{"target-frame", func(o *tailRADynamicOwner) { o.targets[2].info.FrameBytes += 16 }},
		{"target-entry", func(o *tailRADynamicOwner) { o.targets[2].info.Entry += 16 }},
		{"target-span", func(o *tailRADynamicOwner) { o.targets[2].info.CodeBytes -= 4 }},
		{"target-ordinal", func(o *tailRADynamicOwner) { o.targets[2].info.mode.functionIndex++ }},
		{"target-params", func(o *tailRADynamicOwner) { o.targets[2].info.Params = 1 }},
		{"target-job", func(o *tailRADynamicOwner) { o.targets[2].job = o.targets[0].job }},
		{"target-context", func(o *tailRADynamicOwner) { o.targets[2].context = bytes.Clone(o.context) }},
		{"target-anchor", func(o *tailRADynamicOwner) { o.targets[2].directory = bytes.Clone(o.descriptors) }},
		{"target-snapshot", func(o *tailRADynamicOwner) { o.targets[2].snapshot = bytes.Clone(o.targets[2].snapshot) }},
		{"proxy-hole", func(o *tailRADynamicOwner) { o.proxies[2] = o.proxies[0] }},
		{"host-hole", func(o *tailRADynamicOwner) { o.hosts[2] = o.hosts[1] }},
		{"paired-invalid-frame", func(o *tailRADynamicOwner) { o.functions[2].info.FrameBytes += 16; o.targets[2] = o.functions[2] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := (&tailRADynamicSite{owners: []tailRADynamicOwner{f.owner.owner}}).freeze()
			tc.edit(&d.owners[0])
			bad := NativeTailOwner{owner: d.owners[0], trampoline: f.owner.trampoline}
			if err := bad.Validate(); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid paired physical hole accepted", err)
			}
		})
	}
	// All four physical owners authenticate before the borrowed row is read.
	for i, lease := range f.leases {
		lease.err = fmt.Errorf("retired particular owner %d", i)
		f.owner.owner.context[16] ^= 8
		if err := f.owner.Validate(); !errors.Is(err, lease.err) {
			t.Fatal("lease must precede borrowed owner read", i, err)
		}
		if bad, err := f.build(); bad != nil || !errors.Is(err, lease.err) {
			t.Fatal("constructor lease must precede borrowed owner read", i, err)
		}
		f.owner.owner.context[16] ^= 8
		lease.err = nil
	}
}

func TestNativeTailSparseImportedOwnerSignatureStorage(t *testing.T) {
	f := sparseImportedOwnerFixture(t, []bool{true, false}, true, false, []wasm.ValType{wasm.I32})
	for _, i := range []int{0, 1, 3} {
		original := *f.signatures[i]
		for _, change := range []func(*wasm.CompType){
			func(s *wasm.CompType) { s.Fields = []wasm.FieldType{{}} },
			func(s *wasm.CompType) { s.Array = wasm.NewFieldType(wasm.StorageVal(wasm.I64), wasm.Var) },
			func(s *wasm.CompType) { s.Params = []wasm.ValType{wasm.FuncRef} },
		} {
			change(f.signatures[i])
			if bad, err := f.build(); bad != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid callable metadata accepted", i, err)
			}
			*f.signatures[i] = original
		}
		f.signatures[i].Fields = make([]wasm.FieldType, 8192)[:0]
		valid, err := f.build()
		if err != nil || valid.owner.functions[i].signature.Fields != nil || valid.owner.targets[i].signature.Fields != nil {
			t.Fatal("inactive backing retained by opaque owner", i, err)
		}
		*f.signatures[i] = original
	}
	// Private proof inputs also pass through the shared factory freeze path.
	d := (&tailRADynamicSite{owners: []tailRADynamicOwner{f.owner.owner}}).freeze()
	for _, list := range [][]tailRAFunction{d.owners[0].functions, d.owners[0].targets} {
		for i := range list {
			if list[i].signature != nil {
				list[i].signature.Fields = make([]wasm.FieldType, 8192)[:0]
			}
		}
	}
	if _, _, err := d.budget(); err != nil {
		t.Fatal("empty inactive syntax rejected", err)
	}
	snapshot := d.freeze()
	for _, list := range [][]tailRAFunction{snapshot.owners[0].functions, snapshot.owners[0].targets} {
		for _, fn := range list {
			if fn.signature != nil && fn.signature.Fields != nil {
				t.Fatal("inactive backing retained by frozen factory")
			}
		}
	}
	for i := range f.signatures {
		if f.signatures[i] != nil {
			f.signatures[i].Params[0], f.signatures[i].Results[0] = wasm.ExternRef, wasm.FuncRef
		}
		f.signatures[i] = &wasm.CompType{}
	}
	for _, o := range []tailRADynamicOwner{f.owner.owner, snapshot.owners[0]} {
		if err := o.validate(f.owner.trampoline); err != nil {
			t.Fatal("input mutation altered retained signatures", err)
		}
	}
}

func TestNativeTailSparseImportedOwnerMixedCounts(t *testing.T) {
	a := sparseImportedOwnerFixture(t, []bool{true, false, true}, true, false, []wasm.ValType{wasm.I32})
	b, lease, _, _ := sparseOwnerFixture(t, []bool{true, false, true})
	c := a.counts
	c.Owners++
	c.Functions += 3
	c.Values += 3
	c.MaskSlots += 3
	c.NonCallable += 2
	guest := a.guest + len(lease.view)
	// Include an ordinary dense importer as well: only the sparse importer's
	// local holes reduce target signatures, never the defined-only holes.
	d := sparseImportedOwnerFixture(t, []bool{false}, false, false, []wasm.ValType{wasm.F64})
	do := d.owner.owner
	local := do.functions[1]
	dense, err := NewNativeTailImportedOwner(local.published, local.job, do.context, do.descriptors, do.directory, d.entries, []wasm.CompType{*d.signatures[0], *d.signatures[1]}, d.imports, d.owner.trampoline)
	if err != nil {
		t.Fatal(err)
	}
	c.Owners += d.counts.Owners
	c.Functions += d.counts.Functions
	c.Values += d.counts.Values
	c.Targets += d.counts.Targets
	c.TargetValues += d.counts.TargetValues
	c.ProxySlots += d.counts.ProxySlots
	c.Proxies += d.counts.Proxies
	guest += d.guest
	for _, sites := range []int{1, 4} {
		m, code, err := NativeTailFactoryCountsBudget(sites, 3*sites, 2, c, guest)
		am, ac, actualErr := NativeTailDefinedFactoryBudget(sites, 3*sites, 2, []*NativeTailOwner{a.owner, b, dense}, guest)
		if err != nil || actualErr != nil || m != am || code != ac {
			t.Fatal("mixed defined/imported hole counts", m, am, code, ac, err, actualErr)
		}
		if _, _, err := NativeTailFactoryCountsBudget(sites, 3*sites, 2, c, MaxCodeBytes-code); err != nil {
			t.Fatal("exact aggregate code bound", err)
		}
		if _, _, err := NativeTailFactoryCountsBudget(sites, 3*sites, 2, c, MaxCodeBytes-code+1); !errors.Is(err, ErrUnsupported) {
			t.Fatal("aggregate code bound plus one", err)
		}
	}
	for _, edit := range []func(*NativeTailOwnerCounts){
		func(c *NativeTailOwnerCounts) { c.TargetNonCallable = -1 },
		func(c *NativeTailOwnerCounts) { c.TargetNonCallable = c.NonCallable + 1 },
		func(c *NativeTailOwnerCounts) { c.Targets = c.TargetNonCallable - 1 },
		func(c *NativeTailOwnerCounts) { c.TargetValues = 512*(c.Targets-c.TargetNonCallable) + 1 },
		func(c *NativeTailOwnerCounts) { c.Proxies = c.Targets - c.TargetNonCallable + 1 },
	} {
		invalid := c
		edit(&invalid)
		if _, _, err := NativeTailFactoryCountsBudget(1, 3, 2, invalid, guest); !errors.Is(err, ErrUnsupported) {
			t.Fatal("invalid separate target hole counts", invalid, err)
		}
	}
	for _, counts := range [][4]int{{0, 0, 0, 0}, {MaxFunctions + 1, 1, 0, 0}, {2, 0, 1, 0}, {2, 3, 0, 0}, {2, 1, -1, 0}, {2, 1, 2, 0}, {2, 1, 1, 513}, {2, 1, 1, -1}} {
		for _, budget := range []func(int, int, int, int) (int, error){NativeTailSparseImportedOwnerBytes, NativeTailSparseHostOwnerBytes} {
			if _, err := budget(counts[0], counts[1], counts[2], counts[3]); !errors.Is(err, ErrUnsupported) {
				t.Fatal("invalid initial sparse owner counts", counts, err)
			}
		}
	}
	for _, budget := range []func(int, int, int, int) (int, error){NativeTailSparseImportedOwnerBytes, NativeTailSparseHostOwnerBytes} {
		if _, err := budget(2, 1, 1, 512); err != nil {
			t.Fatal("exact callable value bound", err)
		}
	}
	for _, vector := range []bool{false, true} {
		types := make([]wasm.ValType, 256)
		for i := range types {
			types[i] = wasm.I32
		}
		if vector {
			types = types[:128]
			for i := range types {
				types[i] = wasm.V128
			}
		}
		f := sparseImportedOwnerFixture(t, []bool{true, false}, false, false, types)
		m, code, err := NativeTailFactoryCountsBudget(1, 2*len(types), 1, f.counts, f.guest)
		am, ac, actualErr := NativeTailDefinedFactoryBudget(1, 2*len(types), 1, []*NativeTailOwner{f.owner}, f.guest)
		if err != nil || actualErr != nil || m != am || code != ac {
			t.Fatal("max native signature sparse charge", vector, m, am, code, ac, err, actualErr)
		}
	}
}
