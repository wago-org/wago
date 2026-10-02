//go:build ((linux && amd64) || ((linux || darwin) && arm64)) && !tinygo && !wago_guardpage

package wago

import (
	"testing"
)

// Catch clauses are a growable list; the former eight-catch limit was not
// structural. Each tag reaches its own catch and others fall to catch_all.
func TestTryTableWithManyCatches(t *testing.T) {
	inst := tailEHModule(t, manyCatchesWasm)
	for export, want := range map[string]int32{"c0": 100, "c7": 7107, "c15": 15115, "c18": 18118, "c19": 999} {
		if got := invokeI32(t, inst, export); got != want {
			t.Fatalf("%s = %d, want %d", export, got, want)
		}
	}
}

// manyCatchesWasm encodes:
//
//	(module
//	  (tag $t0 (param i32))
//	  (tag $t1 (param i32))
//	  (tag $t2 (param i32))
//	  (tag $t3 (param i32))
//	  (tag $t4 (param i32))
//	  (tag $t5 (param i32))
//	  (tag $t6 (param i32))
//	  (tag $t7 (param i32))
//	  (tag $t8 (param i32))
//	  (tag $t9 (param i32))
//	  (tag $t10 (param i32))
//	  (tag $t11 (param i32))
//	  (tag $t12 (param i32))
//	  (tag $t13 (param i32))
//	  (tag $t14 (param i32))
//	  (tag $t15 (param i32))
//	  (tag $t16 (param i32))
//	  (tag $t17 (param i32))
//	  (tag $t18 (param i32))
//	  (tag $t19 (param i32))
//	  (func $dispatch (param i32) (result i32)
//	(block $b0 (result i32)
//	(block $b1 (result i32)
//	(block $b2 (result i32)
//	(block $b3 (result i32)
//	(block $b4 (result i32)
//	(block $b5 (result i32)
//	(block $b6 (result i32)
//	(block $b7 (result i32)
//	(block $b8 (result i32)
//	(block $b9 (result i32)
//	(block $b10 (result i32)
//	(block $b11 (result i32)
//	(block $b12 (result i32)
//	(block $b13 (result i32)
//	(block $b14 (result i32)
//	(block $b15 (result i32)
//	(block $b16 (result i32)
//	(block $b17 (result i32)
//	(block $b18 (result i32)
//	(block $b19 (result i32)
//	(block $ca
//	  (try_table (catch $t0 $b0) (catch $t1 $b1) (catch $t2 $b2) (catch $t3 $b3) (catch $t4 $b4) (catch $t5 $b5) (catch $t6 $b6) (catch $t7 $b7) (catch $t8 $b8) (catch $t9 $b9) (catch $t10 $b10) (catch $t11 $b11) (catch $t12 $b12) (catch $t13 $b13) (catch $t14 $b14) (catch $t15 $b15) (catch $t16 $b16) (catch $t17 $b17) (catch $t18 $b18) (catch_all $ca)
//	    (if (i32.lt_u (local.get 0) (i32.const 19))
//	      (then
//	        (if (i32.eq (local.get 0) (i32.const 0)) (then (throw $t0 (i32.const 100)))) (if (i32.eq (local.get 0) (i32.const 1)) (then (throw $t1 (i32.const 101)))) (if (i32.eq (local.get 0) (i32.const 2)) (then (throw $t2 (i32.const 102)))) (if (i32.eq (local.get 0) (i32.const 3)) (then (throw $t3 (i32.const 103)))) (if (i32.eq (local.get 0) (i32.const 4)) (then (throw $t4 (i32.const 104)))) (if (i32.eq (local.get 0) (i32.const 5)) (then (throw $t5 (i32.const 105)))) (if (i32.eq (local.get 0) (i32.const 6)) (then (throw $t6 (i32.const 106)))) (if (i32.eq (local.get 0) (i32.const 7)) (then (throw $t7 (i32.const 107)))) (if (i32.eq (local.get 0) (i32.const 8)) (then (throw $t8 (i32.const 108)))) (if (i32.eq (local.get 0) (i32.const 9)) (then (throw $t9 (i32.const 109)))) (if (i32.eq (local.get 0) (i32.const 10)) (then (throw $t10 (i32.const 110)))) (if (i32.eq (local.get 0) (i32.const 11)) (then (throw $t11 (i32.const 111)))) (if (i32.eq (local.get 0) (i32.const 12)) (then (throw $t12 (i32.const 112)))) (if (i32.eq (local.get 0) (i32.const 13)) (then (throw $t13 (i32.const 113)))) (if (i32.eq (local.get 0) (i32.const 14)) (then (throw $t14 (i32.const 114)))) (if (i32.eq (local.get 0) (i32.const 15)) (then (throw $t15 (i32.const 115)))) (if (i32.eq (local.get 0) (i32.const 16)) (then (throw $t16 (i32.const 116)))) (if (i32.eq (local.get 0) (i32.const 17)) (then (throw $t17 (i32.const 117)))) (if (i32.eq (local.get 0) (i32.const 18)) (then (throw $t18 (i32.const 118))))))
//	    (throw $t19 (i32.const 0)))
//	  (return (i32.const -1)))
//	(return (i32.const 999))
//	)
//	(return (i32.add (i32.const 19000))))
//	(return (i32.add (i32.const 18000))))
//	(return (i32.add (i32.const 17000))))
//	(return (i32.add (i32.const 16000))))
//	(return (i32.add (i32.const 15000))))
//	(return (i32.add (i32.const 14000))))
//	(return (i32.add (i32.const 13000))))
//	(return (i32.add (i32.const 12000))))
//	(return (i32.add (i32.const 11000))))
//	(return (i32.add (i32.const 10000))))
//	(return (i32.add (i32.const 9000))))
//	(return (i32.add (i32.const 8000))))
//	(return (i32.add (i32.const 7000))))
//	(return (i32.add (i32.const 6000))))
//	(return (i32.add (i32.const 5000))))
//	(return (i32.add (i32.const 4000))))
//	(return (i32.add (i32.const 3000))))
//	(return (i32.add (i32.const 2000))))
//	(return (i32.add (i32.const 1000))))
//	(return (i32.add (i32.const 0)))
//	  )
//	  (func (export "c0") (result i32) (call $dispatch (i32.const 0)))
//	  (func (export "c7") (result i32) (call $dispatch (i32.const 7)))
//	  (func (export "c15") (result i32) (call $dispatch (i32.const 15)))
//	  (func (export "c18") (result i32) (call $dispatch (i32.const 18)))
//	  (func (export "c19") (result i32) (call $dispatch (i32.const 19)))
//	)
const manyCatchesWasm = "0061736d01000000010e0360017f0060017f017f6000017f0307060102020202020d291400000000000000000000000000000000000000000000000000000000000000000000000000000000071d05026330000102633700020363313500030363313800040363313900050a9e0406f80300027f027f027f027f027f027f027f027f027f027f027f027f027f027f027f027f027f027f027f027f02401f401400001400011300021200031100041000050f00060e00070d00080c00090b000a0a000b09000c08000d07000e06000f050010040011030012020200200041134904402000410046044041e40008000b2000410146044041e50008010b2000410246044041e60008020b2000410346044041e70008030b2000410446044041e80008040b2000410546044041e90008050b2000410646044041ea0008060b2000410746044041eb0008070b2000410846044041ec0008080b2000410946044041ed0008090b2000410a46044041ee00080a0b2000410b46044041ef00080b0b2000410c46044041f000080c0b2000410d46044041f100080d0b2000410e46044041f200080e0b2000410f46044041f300080f0b2000411046044041f40008100b2000411146044041f50008110b2000411246044041f60008120b0b410008130b417f0f0b41e7070f0b41b894016a0f0b41d08c016a0f0b41e884016a0f0b4180fd006a0f0b4198f5006a0f0b41b0ed006a0f0b41c8e5006a0f0b41e0dd006a0f0b41f8d5006a0f0b4190ce006a0f0b41a8c6006a0f0b41c03e6a0f0b41d8366a0f0b41f02e6a0f0b4188276a0f0b41a01f6a0f0b41b8176a0f0b41d00f6a0f0b41e8076a0f0b41006a0f0b0600410010000b0600410710000b0600410f10000b0600411210000b0600411310000b00d201046e616d65010b01000864697370617463680361010015000262300102623102026232030262330402623405026235060262360702623708026238090262390a036231300b036231310c036231320d036231330e036231340f036231351003623136110362313712036231381303623139140263610b5b14000274300102743102027432030274330402743405027435060274360702743708027438090274390a037431300b037431310c037431320d037431330e037431340f037431351003743136110374313712037431381303743139"
