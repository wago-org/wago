import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const updater = join(scriptsDir, "update-website-bench.mjs");
const root = resolve(scriptsDir, "..");

test("benchmark regeneration only replaces the benchmark widget", async () => {
  const work = await mkdtemp(join(tmpdir(), "wago-bench-site-"));
  try {
    const index = join(work, "index.html");
    const amd64 = join(work, "amd64.json");
    const arm64 = join(work, "arm64.json");
    const generalAMD64 = join(work, "general-amd64.json");
    const generalARM64 = join(work, "general-arm64.json");
    await writeFile(index, `<!doctype html>
<body>
            <!-- ░░░ PERFORMANCE ░░░ -->
            <section id="performance">
                <h2 class="section__title">Hand-authored performance heading</h2>
                <p class="section__lead">Hand-authored performance introduction.</p>
                <div class="vs"><div>old generated benchmark</div></div>
                <p class="vs__foot">Hand-authored benchmark footnote.</p>
            </section>
            <!-- ░░░ PLUGINS ░░░ -->
            <section id="packages">Hand-authored plugins</section>
</body>
`);
    const metrics = {
      "CompileFull/tiny": { ns: 10, bytes: 2048, allocs: 3 }, "DraglineCompileFull/tiny": { ns: 9, bytes: 3072, allocs: 4 }, "WazeroCompile/tiny": { ns: 12, bytes: 4096, allocs: 7 },
      "CompileFull/ruby": { ns: 40 }, "WazeroCompile/ruby": { ns: 80 },
      "Instantiate/tiny": { ns: 10, bytes: 512, allocs: 2 }, "DraglineInstantiate/tiny": { ns: 9, bytes: 768, allocs: 3 }, "WazeroInstantiate/tiny": { ns: 12, bytes: 1024, allocs: 5 },
      "Instantiate/fib_rec": { ns: 4 }, "WazeroInstantiate/fib_rec": { ns: 8 },
      "Instantiate/many_funcs": { ns: 6 }, "WazeroInstantiate/many_funcs": { ns: 9 },
      "Instantiate/json-as": { ns: 7 }, "WazeroInstantiate/json-as": { ns: 14 },
      "Exec/tiny.add": { ns: 3 }, "DraglineExec/tiny.add": { ns: 2 }, "WazeroExec/tiny.add": { ns: 4 },
      "ExecCallOverhead_wago": { ns: 100 }, "ExecCallOverhead_wazero": { ns: 104 },
      "ExecHostCallback_wago": { ns: 33 }, "ExecHostRoundtrip_wago": { ns: 99 }, "ExecHostRoundtrip_wazero": { ns: 66 },
      "Exec/nbody.step": { ns: 20 }, "WazeroExec/nbody.step": { ns: 30 },
      "Exec/json-as.deserializeN": { ns: 25 }, "WazeroExec/json-as.deserializeN": { ns: 50 },
      "Exec/json-as-simd.deserializeN": { ns: 18 }, "WazeroExec/json-as-simd.deserializeN": { ns: 36 },
	  "Exec/lua.plugin-workload": { ns: 1e30 }, "WazeroExec/lua.plugin-workload": { ns: 0 },
    };
    for (const name of ["coremark", "blake3", "qoi", "lz4", "zlib", "zstd"]) {
      metrics[`CompileFull/${name}`] = { ns: 100, bytes: 10, allocs: 1 };
      metrics[`WazeroCompile/${name}`] = { ns: 200, bytes: 20, allocs: 2 };
      metrics[`Instantiate/${name}`] = { ns: 30, bytes: 3, allocs: 1 };
      metrics[`WazeroInstantiate/${name}`] = { ns: 60, bytes: 6, allocs: 2 };
    }
    for (const name of [
      "embench-crc32",
      "sightglass-shootout-base64",
      "tacle-bsort",
    ]) {
      metrics[`CompileFull/${name}`] = { ns: 100, bytes: 10, allocs: 1 };
      metrics[`WazeroCompile/${name}`] = { ns: 200, bytes: 20, allocs: 2 };
      metrics[`CommandExec/${name}`] = { ns: 40 };
      metrics[`WazeroCommandExec/${name}`] = { ns: 80 };
    }
    for (const name of [
      "coremark.coremark_run",
      "blake3.blake3_hash",
      "blake3.blake3_keyed_hash",
      "blake3.blake3_derive_key",
      "qoi.qoi_encode_run",
      "qoi.qoi_decode_run",
      "lz4.lz4_compress_run",
      "lz4.lz4_decompress_run",
      "zlib.zlib_inflate_run",
      "zstd.zstd_decompress_run",
    ]) {
      metrics[`Exec/${name}`] = { ns: 40 };
      metrics[`WazeroExec/${name}`] = { ns: 80 };
    }
    for (const key of Object.keys(metrics)) {
      if (key.startsWith("CompileFull/") || key.startsWith("WazeroCompile/")) {
        metrics[key].codeBytes = key.startsWith("Wazero") ? 200 : 100;
      }
    }
    // Missing wazero machine code must remove the pair from both averages.
    metrics["CompileFull/ruby"].codeBytes = 1e30;
    metrics["WazeroCompile/ruby"].codeBytes = 0;
    const modules = Object.fromEntries(
      [...new Set(Object.keys(metrics)
        .filter((key) => key.startsWith("CompileFull/"))
        .map((key) => key.slice("CompileFull/".length)))]
        .map((name) => [name, name.includes("-")
          ? { category: "application", suite: name.split("-")[0], desc: "application workload", bytes: 100 }
          : { category: name === "tiny" ? "micro" : "semantic", bytes: 100 }]),
    );
	modules.lua = { category: "real-large", bytes: 100 };
    const general = {
      compile: [
        { wasm_path: "/tmp/tiny.wasm", runs: [
          { engine: "railshot-native", wall_nanos: 10, peak_rss_bytes: 100 },
          { engine: "dragline-native", wall_nanos: 12, peak_rss_bytes: 110 },
          { engine: "wazero", wall_nanos: 15, peak_rss_bytes: 120 },
          { engine: "cranelift", wall_nanos: 20, peak_rss_bytes: 140 },
          { engine: "v8", wall_nanos: 8, peak_rss_bytes: 160 },
          { engine: "wavm", wall_nanos: 30, peak_rss_bytes: 180 },
        ] },
        { wasm_path: "/tmp/large.wasm", runs: [
          { engine: "railshot-native", wall_nanos: 20, peak_rss_bytes: 120 },
          { engine: "dragline-native", wall_nanos: 24, peak_rss_bytes: 150 },
          { engine: "wazero", wall_nanos: 30, peak_rss_bytes: 180 },
          { engine: "cranelift", wall_nanos: 40, peak_rss_bytes: 240 },
          { engine: "v8", wall_nanos: 16, peak_rss_bytes: 260 },
          { engine: "wavm", wall_nanos: 60, peak_rss_bytes: 380 },
        ] },
      ],
      runtime: [
        { engine: "cranelift", stage: "instantiate", module: "tiny.wasm", ns_per_op: 14 },
        { engine: "cranelift", stage: "exec", module: "tiny.wasm", export: "add", ns_per_op: 5 },
        { engine: "v8", stage: "instantiate", module: "tiny.wasm", ns_per_op: 7 },
        { engine: "v8", stage: "exec", module: "tiny.wasm", export: "add", ns_per_op: 2 },
        { engine: "wavm", stage: "instantiate", module: "tiny.wasm", ns_per_op: 18 },
        { engine: "wavm", stage: "exec", module: "tiny.wasm", export: "add", ns_per_op: 6 },
      ],
    };
    await writeFile(amd64, JSON.stringify({ goos: "linux", goarch: "amd64", modules, metrics }));
    await writeFile(arm64, JSON.stringify({ goos: "darwin", goarch: "arm64", modules, metrics }));
    await writeFile(generalAMD64, JSON.stringify(general));
    await writeFile(generalARM64, JSON.stringify(general));

    const benchmarkEnv = {
      WAGO_BENCH_JSON_AMD64: amd64,
      WAGO_BENCH_JSON_ARM64: arm64,
      WAGO_GENERAL_JSON_AMD64: generalAMD64,
      WAGO_GENERAL_JSON_ARM64: generalARM64,
    };

    runUpdater(work, benchmarkEnv);
    assertDOMContract(await readFile(index, "utf8"));

    runUpdater(work, { ...benchmarkEnv, WAGO_BENCH_UPDATE_ARCH: "amd64" });
    assertDOMContract(await readFile(index, "utf8"));

    runUpdater(work, {
      WAGO_BENCH_JSON_AMD64: amd64,
      WAGO_BENCH_JSON_ARM64: arm64,
      WAGO_GENERAL_JSON_AMD64: join(work, "missing-general-amd64.json"),
      WAGO_GENERAL_JSON_ARM64: join(work, "missing-general-arm64.json"),
    });
    assertDOMContract(await readFile(index, "utf8"));

  } finally {
    await rm(work, { recursive: true, force: true });
  }
});

function runUpdater(websiteDir, extraEnv = {}) {
  const result = spawnSync(process.execPath, [updater], {
    cwd: root,
    env: {
      ...process.env,
      ...extraEnv,
      WAGO_SITE_NOBUILD: "1",
      WAGO_WEBSITE_DIR: websiteDir,
    },
    encoding: "utf8",
  });
  assert.equal(result.status, 0, result.stderr || result.stdout);
}

function assertDOMContract(html) {
  assert.match(html, /Hand-authored performance heading/);
  assert.match(html, /Hand-authored performance introduction/);
  assert.match(html, /<section id="packages">Hand-authored plugins<\/section>/);
  assert.doesNotMatch(html, /old generated benchmark/i);
  assert.match(html, /Hand-authored benchmark footnote/);
  assert.equal(matches(html, /class="vs__archpanel"/g), 2);
  assert.equal(matches(html, /class="vs__main"/g), 2);
  assert.equal(matches(html, /class="vs__toprow"/g), 2);
  assert.equal(matches(html, /class="vs__specs"/g), 2);
  for (const tab of ["compile", "compile-memory", "instantiate", "machine-code", "execution"]) {
    assert.equal(matches(html, new RegExp(`id="perf-(?:amd64|arm64)-tab-${tab}"`, "g")), 2);
  }
  assert.equal(matches(html, />Machine code<\/button>/g), 2);
  assert.equal(matches(html, />Execution<\/button>/g), 2);
  assert.doesNotMatch(html, /Go allocs|allocation objects/i);
  assert.equal(matches(html, /data-engine-toggles/g), 0);
  assert.equal(matches(html, /data-engine-toggle=/g), 0);
  assert.ok(matches(html, /data-engine-row/g) > 80);
  assert.ok(matches(html, /class="vs__delta /g) > 80);
  for (const arch of ["amd64", "arm64"]) {
    const generalStart = html.indexOf(`id="perf-${arch}-panel-general"`);
    const generalEnd = html.indexOf(`id="perf-${arch}-panel-compile"`, generalStart);
    const general = html.slice(generalStart, generalEnd);
    assert.equal(matches(general, /data-engine-row/g), 8);
    for (const label of ["Machine code", "Host → Wasm", "Wasm → host"]) {
      assert.equal(matches(general, new RegExp(`<span class="vs__label">${label}</span>`, "g")), 1);
    }
    assert.doesNotMatch(general, /Application commands|SIMD execution/);
    assert.ok(general.includes('<span class="vs__sub">public entry</span>'));
    assert.ok(general.includes('<span class="vs__sub">typed import callback</span>'));
	const callStart = general.indexOf('<span class="vs__label">Host → Wasm</span>');
	const callEnd = general.indexOf('<div class="vs__row" data-engine-row>', callStart);
	assert.match(general.slice(callStart, callEnd), /vs__delta--tie">parity<\/span>/);
	const callbackStart = general.indexOf('<span class="vs__label">Wasm → host</span>');
	const callbackEnd = general.indexOf('<div class="vs__row" data-engine-row>', callbackStart);
	const callback = general.slice(callbackStart, callbackEnd);
	assert.match(callback, />33ns<\/span>/);
	assert.doesNotMatch(callback, />99ns<\/span>/);
	const machineCodeStart = general.indexOf('<span class="vs__label">Machine code</span>');
	const machineCodeEnd = general.indexOf('<div class="vs__row" data-engine-row>', machineCodeStart);
	const machineCode = general.slice(machineCodeStart, machineCodeEnd);
	assert.match(machineCode, />100 B<\/span>/);
	assert.match(machineCode, />200 B<\/span>/);
	const executionStart = general.indexOf('<span class="vs__label">Execution</span>');
	const executionEnd = general.indexOf('<div class="vs__row" data-engine-row>', executionStart);
	const execution = general.slice(executionStart, executionEnd);
	assert.match(execution, />27\.6ns<\/span>/);
	assert.match(execution, />52\.1ns<\/span>/);
    assert.doesNotMatch(general, /Micro compile mean|Micro startup mean|AS startup mean|Compute execution mean|Tiny compile|Ruby compile|fib_rec startup|Many-function startup|>N-body<|>JSON deserialize</);
  }
  assert.match(html, />[0-9.]+× faster<\/span>/);
  assert.doesNotMatch(html, />1\.0× (?:faster|slower|less|more)<\/span>/);
  assert.match(html, />2× less<\/span>/);
  assert.match(html, /Compile heap/);
  assert.match(html, /<span class="vs__sub">per compile<\/span>/);
  assert.doesNotMatch(html, /Compile memory growth|process RSS/);
  const heapStart = html.indexOf("Compile heap");
  const heapEnd = html.indexOf('<div class="vs__row" data-engine-row>', heapStart);
  const heapRow = html.slice(heapStart, heapEnd);
  for (const engine of ["railshot", "wazero"]) {
    assert.match(heapRow, new RegExp(`data-engine="${engine}"`));
  }
  assert.doesNotMatch(html, /dragline/i);
  assert.doesNotMatch(html, />Railshot</);
  assert.match(html, /<span class="vs__engine">wago<\/span>/);
  assert.doesNotMatch(html, /data-engine="(?:wasmtime|v8|wavm)"|data-engine-toggle="(?:wasmtime|v8|wavm)"/);
	assert.doesNotMatch(html, />0(?:\.0)?ns</);
	const unavailableStart = html.indexOf('<span class="vs__label">lua</span>');
	assert.ok(unavailableStart >= 0);
	const unavailableEnd = html.indexOf('<div class="vs__row" data-engine-row>', unavailableStart);
	const unavailableRow = html.slice(unavailableStart, unavailableEnd);
	assert.match(unavailableRow, /data-engine="railshot"/);
	assert.doesNotMatch(unavailableRow, /data-engine="wazero"/);
  assert.match(html, /Summary metrics · lower is better/);
  assert.match(html, /<span class="vs__sub">fresh process<\/span>/);
  assert.match(html, /<span class="vs__sub">runnable corpus<\/span>/);
  assert.match(html, /<span class="vs__sub">compile \+ instantiate<\/span>/);
  assert.equal(matches(html, /<div class="vs__group">Application corpora<\/div>/g), 8);
  for (const suite of ["embench", "sightglass", "tacle"]) {
    assert.equal(matches(html, new RegExp(`<span class="vs__label">${suite} · `, "g")), 8);
  }
  assert.match(html, /End-to-end latency/);
  assert.match(html, /class="vs__side"[^>]*data-arch-toggle/);
  assert.match(html, /class="vs__stage"/);
  assert.doesNotMatch(html, /class="[^"]*"[^>]+class="/);
}

function matches(text, pattern) {
  return text.match(pattern)?.length ?? 0;
}
