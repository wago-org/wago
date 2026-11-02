"""Bounded correctness fuzzing; no benchmark or performance sample collection."""
import run as q
for pkg,name in [('./src/core/runtime/gc/native','FuzzStructCardDifferential'),('./tests/support/regressioncorpus','FuzzValidateRelativePathAndRustScannerDoNotPanic'),('./tests/tools/regression-corpus','FuzzNormalizeWABTJSONDoesNotPanic'),('./src/wago','FuzzSpecTrapMatchingDoesNotPanic'),('./src/core/compiler/wasm','FuzzDecodeValidateByteBackedDifferentialGenerated')]:
 if q.test('fuzz-30s-'+name,[pkg],'wago_regalloccheck',['-run','^$','-fuzz','^'+name+'$','-fuzztime=30s','-parallel=2']):raise SystemExit(1)
