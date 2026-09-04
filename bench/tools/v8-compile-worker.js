// d8 worker used by compilerharness. new WebAssembly.Module performs V8's
// synchronous baseline compilation; normal tier-up remains enabled to match
// the production engine configuration used by the execution worker.
const [wasmPath, artifactPath] = arguments;
if (!wasmPath || !artifactPath) throw new Error("expected Wasm and artifact paths");
new WebAssembly.Module(readbuffer(wasmPath));
writeFile(artifactPath, "v8\n");
