//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/hex"
	"testing"
)

// GC payload lanes of exception root slots are fixed collector roots at every
// safepoint of the frame, including safepoints before any try_table has
// written the slot. They must be zeroed at frame entry: here a preceding call
// leaves junk words where the slot lives, and the frame collects first.
func TestExceptionRootSlotsZeroedAtFrameEntry(t *testing.T) {
	data, err := hex.DecodeString(exceptionRootSlotEntryWasm)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3), data)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Close()
	if status := compiled.GCNativeRootAdmission(); !status.Required || !status.Exact {
		t.Fatalf("root admission = %+v", status)
	}
	in, err := Instantiate(compiled, InstantiateOptions{GC: GCConfig{Profile: GCProfileTiny, TinyHeapBytes: 256, TinyBlockBytes: 32, TinyCollectEveryAlloc: true, TinyStepEveryAlloc: true, VerifyAfterCollect: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	got, err := in.InvokeValues(context.Background(), "run")
	if err != nil || len(got) != 1 || AsI32(got[0].Bits()) != 180 {
		t.Fatalf("run = %v, %v; want 180", got, err)
	}
}

// exceptionRootSlotEntryWasm encodes:
//
//	(module
//	  (type $s (struct (field i32)))
//	  (tag $t (param (ref null $s)))
//	  ;; leaves non-reference junk words across a large stack frame
//	  (func $dirty (param $seed i64) (result i64) (local $l0 i64) (local $l1 i64) (local $l2 i64) (local $l3 i64) (local $l4 i64) (local $l5 i64) (local $l6 i64) (local $l7 i64) (local $l8 i64) (local $l9 i64) (local $l10 i64) (local $l11 i64) (local $l12 i64) (local $l13 i64) (local $l14 i64) (local $l15 i64) (local $l16 i64) (local $l17 i64) (local $l18 i64) (local $l19 i64) (local $l20 i64) (local $l21 i64) (local $l22 i64) (local $l23 i64) (local $l24 i64) (local $l25 i64) (local $l26 i64) (local $l27 i64) (local $l28 i64) (local $l29 i64) (local $l30 i64) (local $l31 i64) (local $l32 i64) (local $l33 i64) (local $l34 i64) (local $l35 i64) (local $l36 i64) (local $l37 i64) (local $l38 i64) (local $l39 i64) (local $l40 i64) (local $l41 i64) (local $l42 i64) (local $l43 i64) (local $l44 i64) (local $l45 i64) (local $l46 i64) (local $l47 i64) (local $l48 i64) (local $l49 i64) (local $l50 i64) (local $l51 i64) (local $l52 i64) (local $l53 i64) (local $l54 i64) (local $l55 i64) (local $l56 i64) (local $l57 i64) (local $l58 i64) (local $l59 i64)
//	    (local.set $l0 (i64.add (local.get $seed) (i64.const 0)))
//	    (local.set $l1 (i64.add (local.get $seed) (i64.const 8)))
//	    (local.set $l2 (i64.add (local.get $seed) (i64.const 16)))
//	    (local.set $l3 (i64.add (local.get $seed) (i64.const 24)))
//	    (local.set $l4 (i64.add (local.get $seed) (i64.const 32)))
//	    (local.set $l5 (i64.add (local.get $seed) (i64.const 40)))
//	    (local.set $l6 (i64.add (local.get $seed) (i64.const 48)))
//	    (local.set $l7 (i64.add (local.get $seed) (i64.const 56)))
//	    (local.set $l8 (i64.add (local.get $seed) (i64.const 64)))
//	    (local.set $l9 (i64.add (local.get $seed) (i64.const 72)))
//	    (local.set $l10 (i64.add (local.get $seed) (i64.const 80)))
//	    (local.set $l11 (i64.add (local.get $seed) (i64.const 88)))
//	    (local.set $l12 (i64.add (local.get $seed) (i64.const 96)))
//	    (local.set $l13 (i64.add (local.get $seed) (i64.const 104)))
//	    (local.set $l14 (i64.add (local.get $seed) (i64.const 112)))
//	    (local.set $l15 (i64.add (local.get $seed) (i64.const 120)))
//	    (local.set $l16 (i64.add (local.get $seed) (i64.const 128)))
//	    (local.set $l17 (i64.add (local.get $seed) (i64.const 136)))
//	    (local.set $l18 (i64.add (local.get $seed) (i64.const 144)))
//	    (local.set $l19 (i64.add (local.get $seed) (i64.const 152)))
//	    (local.set $l20 (i64.add (local.get $seed) (i64.const 160)))
//	    (local.set $l21 (i64.add (local.get $seed) (i64.const 168)))
//	    (local.set $l22 (i64.add (local.get $seed) (i64.const 176)))
//	    (local.set $l23 (i64.add (local.get $seed) (i64.const 184)))
//	    (local.set $l24 (i64.add (local.get $seed) (i64.const 192)))
//	    (local.set $l25 (i64.add (local.get $seed) (i64.const 200)))
//	    (local.set $l26 (i64.add (local.get $seed) (i64.const 208)))
//	    (local.set $l27 (i64.add (local.get $seed) (i64.const 216)))
//	    (local.set $l28 (i64.add (local.get $seed) (i64.const 224)))
//	    (local.set $l29 (i64.add (local.get $seed) (i64.const 232)))
//	    (local.set $l30 (i64.add (local.get $seed) (i64.const 240)))
//	    (local.set $l31 (i64.add (local.get $seed) (i64.const 248)))
//	    (local.set $l32 (i64.add (local.get $seed) (i64.const 256)))
//	    (local.set $l33 (i64.add (local.get $seed) (i64.const 264)))
//	    (local.set $l34 (i64.add (local.get $seed) (i64.const 272)))
//	    (local.set $l35 (i64.add (local.get $seed) (i64.const 280)))
//	    (local.set $l36 (i64.add (local.get $seed) (i64.const 288)))
//	    (local.set $l37 (i64.add (local.get $seed) (i64.const 296)))
//	    (local.set $l38 (i64.add (local.get $seed) (i64.const 304)))
//	    (local.set $l39 (i64.add (local.get $seed) (i64.const 312)))
//	    (local.set $l40 (i64.add (local.get $seed) (i64.const 320)))
//	    (local.set $l41 (i64.add (local.get $seed) (i64.const 328)))
//	    (local.set $l42 (i64.add (local.get $seed) (i64.const 336)))
//	    (local.set $l43 (i64.add (local.get $seed) (i64.const 344)))
//	    (local.set $l44 (i64.add (local.get $seed) (i64.const 352)))
//	    (local.set $l45 (i64.add (local.get $seed) (i64.const 360)))
//	    (local.set $l46 (i64.add (local.get $seed) (i64.const 368)))
//	    (local.set $l47 (i64.add (local.get $seed) (i64.const 376)))
//	    (local.set $l48 (i64.add (local.get $seed) (i64.const 384)))
//	    (local.set $l49 (i64.add (local.get $seed) (i64.const 392)))
//	    (local.set $l50 (i64.add (local.get $seed) (i64.const 400)))
//	    (local.set $l51 (i64.add (local.get $seed) (i64.const 408)))
//	    (local.set $l52 (i64.add (local.get $seed) (i64.const 416)))
//	    (local.set $l53 (i64.add (local.get $seed) (i64.const 424)))
//	    (local.set $l54 (i64.add (local.get $seed) (i64.const 432)))
//	    (local.set $l55 (i64.add (local.get $seed) (i64.const 440)))
//	    (local.set $l56 (i64.add (local.get $seed) (i64.const 448)))
//	    (local.set $l57 (i64.add (local.get $seed) (i64.const 456)))
//	    (local.set $l58 (i64.add (local.get $seed) (i64.const 464)))
//	    (local.set $l59 (i64.add (local.get $seed) (i64.const 472)))
//	    (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (i64.xor (local.get $l0) (local.get $l1)) (local.get $l2)) (local.get $l3)) (local.get $l4)) (local.get $l5)) (local.get $l6)) (local.get $l7)) (local.get $l8)) (local.get $l9)) (local.get $l10)) (local.get $l11)) (local.get $l12)) (local.get $l13)) (local.get $l14)) (local.get $l15)) (local.get $l16)) (local.get $l17)) (local.get $l18)) (local.get $l19)) (local.get $l20)) (local.get $l21)) (local.get $l22)) (local.get $l23)) (local.get $l24)) (local.get $l25)) (local.get $l26)) (local.get $l27)) (local.get $l28)) (local.get $l29)) (local.get $l30)) (local.get $l31)) (local.get $l32)) (local.get $l33)) (local.get $l34)) (local.get $l35)) (local.get $l36)) (local.get $l37)) (local.get $l38)) (local.get $l39)) (local.get $l40)) (local.get $l41)) (local.get $l42)) (local.get $l43)) (local.get $l44)) (local.get $l45)) (local.get $l46)) (local.get $l47)) (local.get $l48)) (local.get $l49)) (local.get $l50)) (local.get $l51)) (local.get $l52)) (local.get $l53)) (local.get $l54)) (local.get $l55)) (local.get $l56)) (local.get $l57)) (local.get $l58)) (local.get $l59)))
//	  (func $churn (local $i i32)
//	    (loop $l
//	      (drop (struct.new $s (local.get $i)))
//	      (local.set $i (i32.add (local.get $i) (i32.const 1)))
//	      (br_if $l (i32.lt_u (local.get $i) (i32.const 50)))))
//	  ;; collects before its catch_ref slot is first written by a try_table
//	  (func $victim (result i32)
//	    (call $churn)
//	    (block $h (result (ref null $s) exnref)
//	      (try_table (catch_ref $t $h) (throw $t (struct.new $s (i32.const 9))))
//	      (unreachable))
//	    (drop)
//	    (struct.get $s 0 (ref.as_non_null)))
//	  (func (export "run") (result i32) (local $i i32) (local $acc i32)
//	    (loop $l
//	      (drop (call $dirty (i64.const 0x7ff0_0000_0002)))
//	      (local.set $acc (i32.add (local.get $acc) (call $victim)))
//	      (local.set $i (i32.add (local.get $i) (i32.const 1)))
//	      (br_if $l (i32.lt_u (local.get $i) (i32.const 20))))
//	    (local.get $acc)))
const exceptionRootSlotEntryWasm = "0061736d01000000011c065f017f00600163000060017e017e6000006000017f600002630069030504020304040d030100010707010372756e00030af505048f05013c7e200042007c2101200042087c2102200042107c2103200042187c2104200042207c2105200042287c2106200042307c2107200042387c2108200042c0007c2109200042c8007c210a200042d0007c210b200042d8007c210c200042e0007c210d200042e8007c210e200042f0007c210f200042f8007c211020004280017c211120004288017c211220004290017c211320004298017c2114200042a0017c2115200042a8017c2116200042b0017c2117200042b8017c2118200042c0017c2119200042c8017c211a200042d0017c211b200042d8017c211c200042e0017c211d200042e8017c211e200042f0017c211f200042f8017c212020004280027c212120004288027c212220004290027c212320004298027c2124200042a0027c2125200042a8027c2126200042b0027c2127200042b8027c2128200042c0027c2129200042c8027c212a200042d0027c212b200042d8027c212c200042e0027c212d200042e8027c212e200042f0027c212f200042f8027c213020004280037c213120004288037c213220004290037c213320004298037c2134200042a0037c2135200042a8037c2136200042b0037c2137200042b8037c2138200042c0037c2139200042c8037c213a200042d0037c213b200042d8037c213c2001200285200385200485200585200685200785200885200985200a85200b85200c85200d85200e85200f85201085201185201285201385201485201585201685201785201885201985201a85201b85201c85201d85201e85201f85202085202185202285202385202485202585202685202785202885202985202a85202b85202c85202d85202e85202f85203085203185203285203385203485203585203685203785203885203985203a85203b85203c850b1b01017f03402000fb00001a200041016a210020004132490d000b0b1c00100102051f40010100004109fb000008000b000b1ad4fb0200000b2901027f0340428280808080fe1f10001a200110026a2101200041016a210020004114490d000b20010b00f902046e616d65011703000564697274790105636875726e020676696374696d02ba0203003d00047365656401026c3002026c3103026c3204026c3305026c3406026c3507026c3608026c3709026c380a026c390b036c31300c036c31310d036c31320e036c31330f036c313410036c313511036c313612036c313713036c313814036c313915036c323016036c323117036c323218036c323319036c32341a036c32351b036c32361c036c32371d036c32381e036c32391f036c333020036c333121036c333222036c333323036c333424036c333525036c333626036c333727036c333828036c333929036c34302a036c34312b036c34322c036c34332d036c34342e036c34352f036c343630036c343731036c343832036c343933036c353034036c353135036c353236036c353337036c353438036c353539036c35363a036c35373b036c35383c036c3539010100016903020001690103616363031003010100016c0201000168030100016c0404010001730b0401000174"
