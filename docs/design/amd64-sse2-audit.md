# AMD64 optional-instruction audit

Starting revision: `1f137e8e6` (main after #693 and #696).

This is an inventory of the original backend, not a claim of SSE2 compatibility. The #693 admission gate must remain until all emission sites, adapters, artifacts, and plugins are covered.

## Dependency families and fallback plan

| Instructions | Feature | Wasm use | Classification and fallback |
|---|---|---|---|
| VEX.128 scalar ADD/SUB/MUL/DIV/SQRT, XOR/AND; VPXOR | AVX + OS XMM/YMM state | Scalar arithmetic, sign operations, conversions | Encoding optimization; legacy SSE/SSE2 with explicit source preservation |
| ROUNDSS/ROUNDSD | SSE4.1 | Scalar ceil/floor/trunc/nearest | Nontrivial; integer IEEE-754 decomposition preserving signed zero, quieting NaNs, and ties to even |
| VEX.128 MOVDQU, packed arithmetic/logical/compare/convert/shuffle/shift/unpack/pack | AVX + OS state | v128 including spills, locals, arguments, returns, control merges, memory and GC vector values | Encoding optimization; legacy SSE2 and overlap-safe two-operand selection |
| PSHUFB | SSSE3 (AVX for emitted VEX form) | Shuffle, swizzle, bit counts, extension/rearrangement and relaxed swizzle | Nontrivial; fixed shuffle networks for constants; bounded scalar selection for dynamic indices, preserving raw relaxed PSHUFB behavior |
| PABSB/PABSW/PABSD | SSSE3 (AVX for VEX) | Integer vector abs | Compare-negative mask, XOR, subtract |
| PHADDW/PHADDD | SSSE3 (AVX for VEX) | Pairwise addition | Shuffle, add, recombine |
| PMULHRSW | SSSE3 (AVX for VEX) | Core and relaxed q15 multiply | Widen signed multiplication, add rounding bias, arithmetic shift; core saturation fixup, relaxed raw wrap choice |
| PMADDUBSW | SSSE3 (AVX for VEX) | Encoder/plugin vocabulary | Widen, multiply, pairwise add, signed saturation |
| PINSRB/PINSRD/PINSRQ, PEXTRB/PEXTRD/PEXTRQ | SSE4.1 | Splats, lane operations, scalarized SIMD, relaxed dot | SSE2 PINSRW/PEXTRW, MOVD/MOVQ and shifts/shuffles; no change to XMM representation |
| PCMPEQQ | SSE4.1 (AVX for VEX) | i64 lane equality | PCMPEQD then pairwise AND |
| PCMPGTQ | SSE4.2 (AVX for VEX) | i64 signed comparisons, abs/sign handling | Signed high-dword compare; unsigned low-dword compare on equal high dwords |
| PTEST | SSE4.1 (AVX for VEX) | any_true and predicate reductions | PCMPEQB/PMOVMSKB with scalar flags; audit exact PTEST flag use |
| PMULLD | SSE4.1 (AVX for VEX) | i32 lane multiplication | PMULUDQ even/odd lanes, shuffle/recombine low words |
| PMULDQ | SSE4.1 (AVX for VEX) | Signed widening multiplication | PMULUDQ plus signed high-word corrections |
| PMINSB/PMAXSB, PMINUW/PMAXUW, PMINSD/PMAXSD, PMINUD/PMAXUD | SSE4.1 (AVX for VEX) | Integer min/max, saturating conversion | Signed compare or unsigned sign-bit bias, mask select |
| PACKUSDW | SSE4.1 (AVX for VEX) | Unsigned narrowing | Clamp, bias, PACKSSDW, undo bias |
| PBLENDW | SSE4.1 (AVX for VEX) | Lane selection and SIMD rearrangement | AND/ANDN/OR with constant mask |
| VROUNDPS/VROUNDPD | AVX (SSE4.1 legacy equivalent) | Packed rounding and unsigned f64 truncation | Extract scalar lanes, baseline scalar round, reassemble |
| LZCNT | LZCNT/ABM | i32/i64 clz | Already optional after #696: BSR with explicit zero case |
| TZCNT | BMI1 | i32/i64 ctz | Already optional after #696: BSF with explicit zero case |
| POPCNT | POPCNT | i32/i64 popcnt | Already optional after #696: inline 32/64-bit SWAR |
| RORX | BMI2; VEX encoding does not itself imply AVX | Scalar rotate optimization | Existing legacy rotate path; keep actual-emission requirement |
| YMM integer arithmetic, VINSERTI128 | AVX2 + OS state | Optional plugin v256 | Retain plugin gates |
| YMM VMOVDQU and VZEROUPPER | AVX + OS state, not AVX2 | Ordinary bulk memory and table fill | Select XMM/scalar loops when AVX is unavailable |
| YMM floating operations | AVX + OS state (plugin tier currently AVX2) | Optional v256 plugin | Retain declared plugin requirements |
| EVEX ZMM operations, VPTERNLOGD | AVX-512 family + OS state | Optional v512 plugins | Preserve plugin-specific requirements; no core-Wasm dependency |
| VZEROUPPER | AVX + OS state | Bulk memory/table helper boundaries and wide plugin boundaries | Emit only when an AVX-family path was selected |

No FMA or VNNI core lowering found: relaxed madd is explicitly multiply then add/subtract; relaxed dot uses signed scalar products with the documented saturation choice. No AES, PCLMUL, RDRAND, RDSEED, or CMPXCHG16B core emission found. Atomic scalar LOCK operations and ordinary ABI GPR instructions are baseline.

## Non-opcode paths

- `fp.go`, `call.go`, `control.go`, `globals.go`, `localstate.go`, `compile.go`: vector movement and ABI glue emit VEX even without vector arithmetic.
- `memory.go`, `table.go`: inspect optimized bulk paths and their VZEROUPPER boundaries independently of scalar loads/stores.
- `gc_array.go`, `gc_struct.go`, `gc_direct.go`: shared scalar/vector movement and call lowering determines requirements; helpers cannot be omitted from the check.
- `simd.go:emitV128ConstPool`: EmitBytes writes non-executable constant data. Machine-code validation must distinguish literal islands from instructions.
- `src/core/runtime` AMD64 assembly: ordinary moves and MOVOU saves/restores are baseline; CPUID/XGETBV are detection code, not guest instructions.
- `src/wago/scalar_slots_copy_amd64.s`: pre-existing AVX2 narrow-slot copy helper has a cached host/OS gate and scalar fallback. It is runtime code rather than serialized guest code.
- `plugin_machine.go` and `codegen/amd64`: plugins expose a separate optional wide-instruction vocabulary and declared feature requirements. Preserve these requirements during consolidation.
- `src/wago/codec.go`: artifact version 2 currently relies on the global modern baseline and persists bit-count/BMI2 requirements separately from plugin declarations. A future loader must not interpret old version-2 code as baseline code merely because it lacks new bits.
- Standard Go detection uses CPUID/XGETBV; TinyGo Linux reads exact cpuinfo tokens. Detection failure must continue to fail closed while the gate is needed.

## Original encoder entry points and backend callers

Paths in the encoder column are relative to `src/core/encoder/amd64`; callers are relative to `src/core/compiler/backend/railshot/amd64`. This inventory includes exposed encoder operations with no direct Railshot caller so plugin vocabulary is visible.

| Encoder entry point | Definition | Direct backend callers |
|---|---|---|
| `Lzcnt` | `asm.go:532` | `emit.go:822` |
| `Pextrb` | `asm_sse.go:278` | `simd.go:1603`, `simd.go:1605`, `simd.go:1609`, `simd.go:1611`, `simd.go:2106`, `simd.go:2414` |
| `Pextrd` | `asm_sse.go:284` | `simd.go:1688`, `simd.go:2122`, `simd.go:2418` |
| `Pextrq` | `asm_sse.go:287` | `simd.go:1322`, `simd.go:2127`, `simd.go:2420` |
| `Pinsrb` | `asm_sse.go:266` | `simd.go:2150`, `simd.go:2382` |
| `Pinsrd` | `asm_sse.go:272` | `simd.go:1690`, `simd.go:2158`, `simd.go:2171`, `simd.go:2386` |
| `Pinsrq` | `asm_sse.go:275` | `memory.go:1478`, `memory.go:1479`, `simd.go:339`, `simd.go:1326`, `simd.go:2162`, `simd.go:2180`, `simd.go:2388` |
| `Popcnt` | `asm.go:534` | `emit.go:853` |
| `Rorx` | `asm_sse.go:140` | `emit.go:625` |
| `Round` | `asm_sse.go:649` | `fp.go:560` |
| `Tzcnt` | `asm.go:533` | `emit.go:839` |
| `VFAdd` | `asm_sse.go:196` | `driver.go:509`, `driver.go:512`, `driver.go:550`, `driver.go:553` |
| `VFCmpPacked` | `asm_sse.go:246` | `simd.go:837`, `simd.go:950`, `simd.go:974`, `simd.go:993`, `simd.go:1840` |
| `VFDiv` | `asm_sse.go:199` | `driver.go:524`, `driver.go:527`, `driver.go:565`, `driver.go:568` |
| `VFMemIdx` | `asm_sse.go:203` | `fp.go:437`, `fp.go:451` |
| `VFMul` | `asm_sse.go:198` | `driver.go:519`, `driver.go:522`, `driver.go:560`, `driver.go:563` |
| `VFPackedAdd` | `asm_sse.go:215` | `simd.go:896`, `simd.go:1101`, `simd.go:2800`, `simd.go:2822` |
| `VFPackedDiv` | `asm_sse.go:218` | `simd.go:2806`, `simd.go:2828` |
| `VFPackedMax` | `asm_sse.go:220` | `simd.go:827`, `simd.go:2498`, `simd.go:2502`, `simd.go:2814`, `simd.go:2836` |
| `VFPackedMin` | `asm_sse.go:219` | `simd.go:825`, `simd.go:2496`, `simd.go:2500`, `simd.go:2812`, `simd.go:2834` |
| `VFPackedMul` | `asm_sse.go:217` | `simd.go:886`, `simd.go:1099`, `simd.go:2804`, `simd.go:2826` |
| `VFPackedSqrt` | `asm_sse.go:221` | `simd.go:2798`, `simd.go:2820` |
| `VFPackedSub` | `asm_sse.go:216` | `simd.go:890`, `simd.go:1073`, `simd.go:2802`, `simd.go:2824` |
| `VFRoundPacked` | `asm_sse.go:227` | `simd.go:512`, `simd.go:929` |
| `VFSqrt` | `asm_avx2_compat.go:5` | `fp.go:525` |
| `VFSub` | `asm_sse.go:197` | `driver.go:514`, `driver.go:517`, `driver.go:555`, `driver.go:558` |
| `VMovdqu` | `asm_sse.go:308` | `driver.go:786`, `driver.go:1053`, `driver.go:1058`, `simd.go:52`, `simd.go:129`, `simd.go:591`, `simd.go:949`, `simd.go:973`, `simd.go:990` |
| `VMovdquLoadDisp` | `asm_sse.go:296` | `call.go:1188`, `call.go:1271`, `call.go:1641`, `compile.go:4135`, `compile.go:4137`, `compile.go:4536`, `globals.go:99`, `localstate.go:111`, `simd.go:38`, `simd.go:43`, `simd.go:785` |
| `VMovdquLoadIdx` | `asm_sse.go:302` | `memory.go:1290`, `memory.go:1329`, `memory.go:1392`, `memory.go:1817`, `simd.go:2217` |
| `VMovdquStoreDisp` | `asm_sse.go:299` | `call.go:1189`, `compile.go:4138`, `compile.go:4537`, `control.go:902`, `control.go:1005`, `driver.go:1097`, `fp.go:131`, `fuse.go:110`, `globals.go:160`, `localstate.go:94` |
| `VMovdquStoreIdx` | `asm_sse.go:305` | `memory.go:1291`, `memory.go:1330`, `memory.go:1395`, `memory.go:1481`, `memory.go:1822`, `simd.go:2340` |
| `VMovmskpd` | `asm_avx2_compat.go:87` | `simd.go:2025` |
| `VMovmskps` | `asm_avx2_compat.go:83` | `simd.go:2016` |
| `VPabsb` | `asm_sse.go:496` | `simd.go:1418`, `simd.go:2876` |
| `VPabsd` | `asm_sse.go:498` | `simd.go:2886` |
| `VPabsw` | `asm_sse.go:497` | `simd.go:2882` |
| `VPacksswb` | `asm_avx2_compat.go:63` | `simd.go:2003` |
| `VPaddb` | `asm_sse.go:467` | `simd.go:537`, `simd.go:2626` |
| `VPaddd` | `asm_sse.go:469` | `simd.go:998`, `simd.go:2744` |
| `VPadddMemDisp` | `asm_sse.go:588` | `simd.go:2744` |
| `VPaddq` | `asm_sse.go:470` | `simd.go:1370`, `simd.go:1373`, `simd.go:2768` |
| `VPaddsb` | `asm_sse.go:471` | `simd.go:2628` |
| `VPaddsw` | `asm_sse.go:473` | `simd.go:2684` |
| `VPaddusb` | `asm_sse.go:472` | `simd.go:624`, `simd.go:2630` |
| `VPaddusw` | `asm_sse.go:474` | `simd.go:2686` |
| `VPaddw` | `asm_sse.go:468` | `simd.go:2682` |
| `VPand` | `asm_sse.go:483` | `simd.go:529`, `simd.go:530`, `simd.go:867`, `simd.go:1090`, `simd.go:1246`, `simd.go:1247`, `simd.go:1271`, `simd.go:1276`, `simd.go:1281`, `simd.go:1713`, `simd.go:1723`, `simd.go:2902` |
| `VPandMemDisp` | `asm_sse.go:592` | `simd.go:2902` |
| `VPandn` | `asm_sse.go:484` | `simd.go:868`, `simd.go:1724`, `simd.go:1957`, `simd.go:2912` |
| `VPavgb` | `asm_sse.go:627` | `simd.go:2652` |
| `VPavgw` | `asm_sse.go:632` | `simd.go:2706` |
| `VPblendw` | `asm_sse.go:643` | No direct call; encoder/plugin vocabulary |
| `VPcmpeqb` | `asm_sse.go:487` | `simd.go:486`, `simd.go:1417`, `simd.go:1742`, `simd.go:1763`, `simd.go:1791`, `simd.go:1984`, `simd.go:2524`, `simd.go:2526`, `simd.go:2530`, `simd.go:2534`, `simd.go:2538`, `simd.go:2542` |
| `VPcmpeqd` | `asm_sse.go:489` | `simd.go:987`, `simd.go:1820`, `simd.go:1853`, `simd.go:1988`, `simd.go:2564`, `simd.go:2566`, `simd.go:2570`, `simd.go:2574`, `simd.go:2578`, `simd.go:2582` |
| `VPcmpeqq` | `asm_sse.go:490` | `simd.go:1990`, `simd.go:2782`, `simd.go:2784` |
| `VPcmpeqw` | `asm_sse.go:488` | `simd.go:1244`, `simd.go:1531`, `simd.go:1710`, `simd.go:1712`, `simd.go:1986`, `simd.go:2544`, `simd.go:2546`, `simd.go:2550`, `simd.go:2554`, `simd.go:2558`, `simd.go:2562` |
| `VPcmpgtb` | `asm_sse.go:491` | `simd.go:2528`, `simd.go:2532`, `simd.go:2536`, `simd.go:2540` |
| `VPcmpgtd` | `asm_sse.go:493` | `simd.go:1560`, `simd.go:2257`, `simd.go:2568`, `simd.go:2572`, `simd.go:2576`, `simd.go:2580` |
| `VPcmpgtq` | `asm_avx2_compat.go:59` | `simd.go:1343`, `simd.go:1814`, `simd.go:1816` |
| `VPcmpgtw` | `asm_sse.go:492` | `simd.go:2548`, `simd.go:2552`, `simd.go:2556`, `simd.go:2560` |
| `VPhaddd` | `asm_sse.go:609` | `simd.go:1547` |
| `VPhaddw` | `asm_sse.go:608` | No direct call; encoder/plugin vocabulary |
| `VPmaddubsw` | `asm_avx2_compat.go:79` | `simd.go:1420`, `simd.go:1422` |
| `VPmaddwd` | `asm_sse.go:621` | `simd.go:1533`, `simd.go:2758` |
| `VPmaxsb` | `asm_sse.go:625` | `simd.go:2646` |
| `VPmaxsd` | `asm_sse.go:635` | `simd.go:997`, `simd.go:2754` |
| `VPmaxsw` | `asm_sse.go:630` | `simd.go:2702` |
| `VPmaxub` | `asm_sse.go:626` | `simd.go:2530`, `simd.go:2542`, `simd.go:2648` |
| `VPmaxud` | `asm_sse.go:636` | `simd.go:2570`, `simd.go:2582`, `simd.go:2756` |
| `VPmaxuw` | `asm_sse.go:631` | `simd.go:2550`, `simd.go:2562`, `simd.go:2704` |
| `VPminsb` | `asm_sse.go:623` | `simd.go:2642` |
| `VPminsd` | `asm_sse.go:633` | `simd.go:2750` |
| `VPminsw` | `asm_sse.go:628` | `simd.go:2698` |
| `VPminub` | `asm_sse.go:624` | `simd.go:2534`, `simd.go:2538`, `simd.go:2644` |
| `VPminud` | `asm_sse.go:634` | `simd.go:2574`, `simd.go:2578`, `simd.go:2752` |
| `VPminuw` | `asm_sse.go:629` | `simd.go:2554`, `simd.go:2558`, `simd.go:2700` |
| `VPmovmskb` | `asm_sse.go:494` | `simd.go:1878`, `simd.go:1977`, `simd.go:2006` |
| `VPmuldq` | `asm_sse.go:641` | `simd.go:1593` |
| `VPmulhrsw` | `asm_sse.go:610` | `simd.go:1718`, `simd.go:2504` |
| `VPmulld` | `asm_sse.go:640` | `simd.go:1520`, `simd.go:2748` |
| `VPmullw` | `asm_sse.go:622` | `simd.go:1459`, `simd.go:2696` |
| `VPmuludq` | `asm_sse.go:642` | `simd.go:1367`, `simd.go:1369`, `simd.go:1372`, `simd.go:1595` |
| `VPor` | `asm_sse.go:485` | `simd.go:710`, `simd.go:869`, `simd.go:1072`, `simd.go:1173`, `simd.go:1221`, `simd.go:1539`, `simd.go:1725`, `simd.go:2921` |
| `VPorMemDisp` | `asm_sse.go:596` | `simd.go:2921` |
| `VPpackssdw` | `asm_sse.go:618` | `simd.go:2664` |
| `VPpacksswb` | `asm_sse.go:617` | `simd.go:1241`, `simd.go:2608` |
| `VPpackusdw` | `asm_sse.go:620` | `simd.go:2666` |
| `VPpackuswb` | `asm_sse.go:619` | `simd.go:1249`, `simd.go:2610` |
| `VPshufb` | `asm_sse.go:604` | `simd.go:534`, `simd.go:535`, `simd.go:550`, `simd.go:627`, `simd.go:2476` |
| `VPshufbMemIdx` | `asm_sse.go:637` | No direct call; encoder/plugin vocabulary |
| `VPshufbRipPlaceholder` | `asm_sse.go:605` | `simd.go:553` |
| `VPslld` | `asm_sse.go:508` | `simd.go:2724` |
| `VPslldImm` | `asm_avx2_compat.go:71` | `simd.go:1171`, `simd.go:1862`, `simd.go:2724` |
| `VPsllq` | `asm_sse.go:514` | `simd.go:2738` |
| `VPsllqImm` | `asm_avx2_compat.go:75` | `simd.go:1371`, `simd.go:1860`, `simd.go:2738` |
| `VPsllw` | `asm_sse.go:502` | `simd.go:2620`, `simd.go:2676` |
| `VPsllwImm` | `asm_avx2_compat.go:67` | `simd.go:1269`, `simd.go:2676` |
| `VPsrad` | `asm_sse.go:510` | `simd.go:2726` |
| `VPsradImm` | `asm_sse.go:531` | `simd.go:979`, `simd.go:1473`, `simd.go:1505`, `simd.go:1506`, `simd.go:2247`, `simd.go:2726` |
| `VPsraw` | `asm_sse.go:504` | `simd.go:2622`, `simd.go:2678` |
| `VPsrawImm` | `asm_sse.go:525` | `simd.go:1225`, `simd.go:1226`, `simd.go:1393`, `simd.go:1444`, `simd.go:1445`, `simd.go:2239`, `simd.go:2678` |
| `VPsrld` | `asm_sse.go:509` | `simd.go:1185` |
| `VPsrldImm` | `asm_sse.go:537` | `simd.go:847`, `simd.go:988`, `simd.go:1095`, `simd.go:1172`, `simd.go:1185`, `simd.go:1858` |
| `VPsrlq` | `asm_sse.go:515` | `simd.go:2742` |
| `VPsrlqImm` | `asm_sse.go:543` | `simd.go:845`, `simd.go:1366`, `simd.go:1368`, `simd.go:1856`, `simd.go:2742` |
| `VPsrlw` | `asm_sse.go:503` | `simd.go:2624`, `simd.go:2680` |
| `VPsrlwImm` | `asm_sse.go:519` | `simd.go:523`, `simd.go:1245`, `simd.go:1274`, `simd.go:1279`, `simd.go:1532`, `simd.go:2680` |
| `VPsubb` | `asm_sse.go:475` | `simd.go:1285`, `simd.go:2632`, `simd.go:2878` |
| `VPsubd` | `asm_sse.go:477` | `simd.go:2746`, `simd.go:2888` |
| `VPsubq` | `asm_sse.go:478` | `simd.go:1345`, `simd.go:2770`, `simd.go:2892` |
| `VPsubsb` | `asm_sse.go:479` | `simd.go:2634` |
| `VPsubsw` | `asm_sse.go:481` | `simd.go:2690` |
| `VPsubusb` | `asm_sse.go:480` | `simd.go:2636` |
| `VPsubusw` | `asm_sse.go:482` | `simd.go:2692` |
| `VPsubw` | `asm_sse.go:476` | `simd.go:2688`, `simd.go:2884` |
| `VPtest` | `asm_sse.go:495` | `simd.go:1886`, `simd.go:1922` |
| `VPunpckhbw` | `asm_sse.go:614` | `simd.go:1224`, `simd.go:1231`, `simd.go:1389`, `simd.go:1401`, `simd.go:1438`, `simd.go:1439`, `simd.go:1450`, `simd.go:1451` |
| `VPunpckhdq` | `asm_sse.go:616` | `simd.go:641`, `simd.go:1562`, `simd.go:1568` |
| `VPunpckhwd` | `asm_sse.go:615` | `simd.go:1469`, `simd.go:1481`, `simd.go:1499`, `simd.go:1500`, `simd.go:1511`, `simd.go:1512`, `simd.go:1544` |
| `VPunpcklbw` | `asm_sse.go:611` | `simd.go:1223`, `simd.go:1230`, `simd.go:1391`, `simd.go:1403`, `simd.go:1441`, `simd.go:1442`, `simd.go:1453`, `simd.go:1454`, `simd.go:2238`, `simd.go:2243` |
| `VPunpckldq` | `asm_sse.go:613` | `simd.go:639`, `simd.go:1068`, `simd.go:1564`, `simd.go:1570`, `simd.go:2258`, `simd.go:2264` |
| `VPunpcklwd` | `asm_sse.go:612` | `simd.go:1471`, `simd.go:1483`, `simd.go:1502`, `simd.go:1503`, `simd.go:1514`, `simd.go:1515`, `simd.go:1543`, `simd.go:2246`, `simd.go:2251` |
| `VPxor` | `asm_sse.go:486` | `fp.go:678`, `fp.go:695`, `fp.go:704`, `simd.go:32`, `simd.go:125`, `simd.go:487`, `simd.go:496`, `simd.go:924`, `simd.go:980`, `simd.go:985`, `simd.go:995`, `simd.go:996`, `simd.go:1065`, `simd.go:1229`, `simd.go:1284`, `simd.go:1342`, `simd.go:1344`, `simd.go:1399`, `simd.go:1448`, `simd.go:1479`, `simd.go:1509`, `simd.go:1542`, `simd.go:1557`, `simd.go:1634`, `simd.go:1743`, `simd.go:1764`, `simd.go:1792`, `simd.go:1821`, `simd.go:1973`, `simd.go:2242`, `simd.go:2250`, `simd.go:2255`, `simd.go:2263`, `simd.go:2923` |
| `VPxorMemDisp` | `asm_sse.go:600` | `simd.go:2923` |
| `VShufps` | `asm_avx2_compat.go:55` | `simd.go:643`, `simd.go:645`, `simd.go:933` |
| `VSseRRR` | `asm_sse.go:253` | `fp.go:546`, `fp.go:600`, `fp.go:601`, `fp.go:603`, `fp.go:605`, `simd.go:839`, `simd.go:841`, `simd.go:843`, `simd.go:849`, `simd.go:925`, `simd.go:927`, `simd.go:931`, `simd.go:952`, `simd.go:954`, `simd.go:975`, `simd.go:976`, `simd.go:978`, `simd.go:986`, `simd.go:992`, `simd.go:1869` |
| `VZeroUpper` | `asm_sse.go:465` | `memory.go:1386`, `table.go:690` |
| `Vcvtdq2pd` | `asm_avx2_compat.go:13` | `simd.go:1045` |
| `Vcvtdq2ps` | `asm_avx2_compat.go:9` | `simd.go:989`, `simd.go:1047`, `simd.go:1096`, `simd.go:1097` |
| `Vcvtpd2ps` | `asm_avx2_compat.go:21` | `simd.go:1016` |
| `Vcvtps2pd` | `asm_avx2_compat.go:17` | `simd.go:1028` |
| `Vcvttpd2dq` | `asm_sse.go:242` | `simd.go:955` |
| `Vcvttps2dq` | `asm_sse.go:238` | `simd.go:977`, `simd.go:991`, `simd.go:994` |
| `YFCmpPacked` | `asm_sse.go:382` | No direct call; encoder/plugin vocabulary |
| `YFPackedAdd` | `asm_sse.go:353` | No direct call; encoder/plugin vocabulary |
| `YFPackedDiv` | `asm_sse.go:362` | No direct call; encoder/plugin vocabulary |
| `YFPackedMax` | `asm_sse.go:368` | No direct call; encoder/plugin vocabulary |
| `YFPackedMin` | `asm_sse.go:365` | No direct call; encoder/plugin vocabulary |
| `YFPackedMul` | `asm_sse.go:359` | No direct call; encoder/plugin vocabulary |
| `YFPackedSqrt` | `asm_sse.go:371` | No direct call; encoder/plugin vocabulary |
| `YFPackedSub` | `asm_sse.go:356` | No direct call; encoder/plugin vocabulary |
| `YFRoundPacked` | `asm_sse.go:374` | No direct call; encoder/plugin vocabulary |
| `YInsertI128` | `asm_sse.go:350` | `plugin_machine.go:96` |
| `YMovdqu` | `asm_sse.go:326` | No direct call; encoder/plugin vocabulary |
| `YMovdquLoadDisp` | `asm_sse.go:314` | `plugin_machine.go:277`, `table.go:684` |
| `YMovdquLoadIdx` | `asm_sse.go:320` | `memory.go:1378`, `plugin_machine.go:108` |
| `YMovdquStoreDisp` | `asm_sse.go:317` | `fp.go:123`, `table.go:686` |
| `YMovdquStoreIdx` | `asm_sse.go:323` | `memory.go:1381`, `plugin_machine.go:121` |
| `YPabsb` | `asm_sse.go:408` | No direct call; encoder/plugin vocabulary |
| `YPabsd` | `asm_sse.go:410` | No direct call; encoder/plugin vocabulary |
| `YPabsw` | `asm_sse.go:409` | No direct call; encoder/plugin vocabulary |
| `YPaddb` | `asm_sse.go:330` | No direct call; encoder/plugin vocabulary |
| `YPaddd` | `asm_sse.go:335` | No direct call; encoder/plugin vocabulary |
| `YPaddq` | `asm_sse.go:338` | No direct call; encoder/plugin vocabulary |
| `YPaddsb` | `asm_sse.go:389` | No direct call; encoder/plugin vocabulary |
| `YPaddsw` | `asm_sse.go:391` | No direct call; encoder/plugin vocabulary |
| `YPaddusb` | `asm_sse.go:390` | No direct call; encoder/plugin vocabulary |
| `YPaddusw` | `asm_sse.go:392` | No direct call; encoder/plugin vocabulary |
| `YPaddw` | `asm_sse.go:332` | No direct call; encoder/plugin vocabulary |
| `YPand` | `asm_sse.go:340` | No direct call; encoder/plugin vocabulary |
| `YPandn` | `asm_sse.go:341` | No direct call; encoder/plugin vocabulary |
| `YPavgb` | `asm_sse.go:433` | No direct call; encoder/plugin vocabulary |
| `YPavgw` | `asm_sse.go:442` | No direct call; encoder/plugin vocabulary |
| `YPcmpeqb` | `asm_sse.go:344` | No direct call; encoder/plugin vocabulary |
| `YPcmpeqd` | `asm_sse.go:398` | No direct call; encoder/plugin vocabulary |
| `YPcmpeqq` | `asm_sse.go:399` | No direct call; encoder/plugin vocabulary |
| `YPcmpeqw` | `asm_sse.go:397` | No direct call; encoder/plugin vocabulary |
| `YPcmpgtb` | `asm_sse.go:402` | No direct call; encoder/plugin vocabulary |
| `YPcmpgtd` | `asm_sse.go:404` | No direct call; encoder/plugin vocabulary |
| `YPcmpgtq` | `asm_sse.go:405` | No direct call; encoder/plugin vocabulary |
| `YPcmpgtw` | `asm_sse.go:403` | No direct call; encoder/plugin vocabulary |
| `YPhaddd` | `asm_sse.go:417` | No direct call; encoder/plugin vocabulary |
| `YPhaddw` | `asm_sse.go:414` | No direct call; encoder/plugin vocabulary |
| `YPmaddwd` | `asm_sse.go:423` | No direct call; encoder/plugin vocabulary |
| `YPmaxsb` | `asm_sse.go:429` | No direct call; encoder/plugin vocabulary |
| `YPmaxsd` | `asm_sse.go:449` | No direct call; encoder/plugin vocabulary |
| `YPmaxsw` | `asm_sse.go:438` | No direct call; encoder/plugin vocabulary |
| `YPmaxub` | `asm_sse.go:432` | No direct call; encoder/plugin vocabulary |
| `YPmaxud` | `asm_sse.go:452` | No direct call; encoder/plugin vocabulary |
| `YPmaxuw` | `asm_sse.go:439` | No direct call; encoder/plugin vocabulary |
| `YPminsb` | `asm_sse.go:425` | No direct call; encoder/plugin vocabulary |
| `YPminsd` | `asm_sse.go:443` | No direct call; encoder/plugin vocabulary |
| `YPminsw` | `asm_sse.go:434` | No direct call; encoder/plugin vocabulary |
| `YPminub` | `asm_sse.go:428` | No direct call; encoder/plugin vocabulary |
| `YPminud` | `asm_sse.go:446` | No direct call; encoder/plugin vocabulary |
| `YPminuw` | `asm_sse.go:435` | No direct call; encoder/plugin vocabulary |
| `YPmovmskb` | `asm_sse.go:347` | No direct call; encoder/plugin vocabulary |
| `YPmulhrsw` | `asm_sse.go:420` | No direct call; encoder/plugin vocabulary |
| `YPmulld` | `asm_sse.go:337` | No direct call; encoder/plugin vocabulary |
| `YPmullw` | `asm_sse.go:334` | No direct call; encoder/plugin vocabulary |
| `YPmuludq` | `asm_sse.go:424` | No direct call; encoder/plugin vocabulary |
| `YPor` | `asm_sse.go:342` | No direct call; encoder/plugin vocabulary |
| `YPshufb` | `asm_sse.go:411` | No direct call; encoder/plugin vocabulary |
| `YPslld` | `asm_sse.go:458` | No direct call; encoder/plugin vocabulary |
| `YPsllq` | `asm_sse.go:461` | No direct call; encoder/plugin vocabulary |
| `YPsllqImm` | `asm_sse.go:585` | No direct call; encoder/plugin vocabulary |
| `YPsllw` | `asm_sse.go:455` | No direct call; encoder/plugin vocabulary |
| `YPsrad` | `asm_sse.go:460` | No direct call; encoder/plugin vocabulary |
| `YPsraw` | `asm_sse.go:457` | No direct call; encoder/plugin vocabulary |
| `YPsrld` | `asm_sse.go:459` | No direct call; encoder/plugin vocabulary |
| `YPsrldImm` | `asm_sse.go:583` | No direct call; encoder/plugin vocabulary |
| `YPsrlq` | `asm_sse.go:462` | No direct call; encoder/plugin vocabulary |
| `YPsrlqImm` | `asm_sse.go:584` | No direct call; encoder/plugin vocabulary |
| `YPsrlw` | `asm_sse.go:456` | No direct call; encoder/plugin vocabulary |
| `YPsrlwImm` | `asm_sse.go:586` | No direct call; encoder/plugin vocabulary |
| `YPsubb` | `asm_sse.go:331` | No direct call; encoder/plugin vocabulary |
| `YPsubd` | `asm_sse.go:336` | No direct call; encoder/plugin vocabulary |
| `YPsubq` | `asm_sse.go:339` | No direct call; encoder/plugin vocabulary |
| `YPsubsb` | `asm_sse.go:393` | No direct call; encoder/plugin vocabulary |
| `YPsubsw` | `asm_sse.go:395` | No direct call; encoder/plugin vocabulary |
| `YPsubusb` | `asm_sse.go:394` | No direct call; encoder/plugin vocabulary |
| `YPsubusw` | `asm_sse.go:396` | No direct call; encoder/plugin vocabulary |
| `YPsubw` | `asm_sse.go:333` | No direct call; encoder/plugin vocabulary |
| `YPxor` | `asm_sse.go:343` | No direct call; encoder/plugin vocabulary |
| `YSseRRR` | `asm_sse.go:385` | No direct call; encoder/plugin vocabulary |
| `ZMovdqu64LoadIdx` | `asm_avx512.go:64` | `plugin_machine.go:132` |
| `ZMovdqu64StoreIdx` | `asm_avx512.go:68` | `plugin_machine.go:145` |
| `ZPternlogd` | `asm_avx512.go:82` | No direct call; encoder/plugin vocabulary |
| `ZSIMDRR` | `asm_avx512.go:78` | No direct call; encoder/plugin vocabulary |
| `ZSIMDRRR` | `asm_avx512.go:74` | No direct call; encoder/plugin vocabulary |

## Migration checkpoint

Implemented after the audit:

- Value-based backend capability selection, with an explicit-selection flag
  while the default remains the old modern baseline. No global test CPU override
  or invocation-time dispatch was added.
- Legacy scalar arithmetic, sign operations, sqrt, and conversion zeroing.
- All eight scalar rounding operations using integer bit decomposition. This
  preserves signed zero, handles every exponent, quiets NaNs, and does not
  consult or modify MXCSR. The SSE4.1 lowering is retained for selected hosts.
- SSE2 unaligned vector movement for ABI/spill/bulk-memory use, and SSE2 fill
  pattern construction. AVX YMM bulk paths remain optional compile-time paths.
- Existing bit-count fallback selection is constrained by explicit CPU masks.
- Incomplete SIMD/plugin profiles fail closed, including vector types that do
  not use SIMD opcodes. The public #693 gate has not been relaxed.

Still required before claiming the requested final SSE2 baseline:

1. Complete core and relaxed SIMD lowerings, including optimizer-only paths.
2. Consolidate host detection and TinyGo detection into the immutable mask.
3. Track actual emitted optional instructions in artifacts; version the format
   and reject old ambiguous requirements conservatively.
4. Expand instruction decoding checks beyond scalar stubs and scalar functions,
   distinguishing code from literal data and jump tables.
5. Complete the requested differential SIMD suites and performance matrix.
6. Narrow the public gate only after those checks pass.

The checkpoint does not satisfy the full SSE2 acceptance criteria. No artifact
format changes or weaker artifact-admission rules have been made yet.
