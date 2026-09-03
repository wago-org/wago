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

test("benchmark regeneration preserves the fixed-height architecture DOM", async () => {
  const work = await mkdtemp(join(tmpdir(), "wago-bench-site-"));
  try {
    const index = join(work, "index.html");
    const amd64 = join(work, "amd64.json");
    const arm64 = join(work, "arm64.json");
    const generalAMD64 = join(work, "general-amd64.json");
    const generalARM64 = join(work, "general-arm64.json");
    const rssAMD64 = join(work, "rss-amd64.json");
    await writeFile(index, `<!doctype html>
<body>
            <!-- ░░░ PERFORMANCE ░░░ -->
            <section id="performance"></section>
            <!-- ░░░ ARCHITECTURE ░░░ -->
            <section id="architecture"></section>
</body>
`);
    const metrics = {
      "CompileFull/tiny": { ns: 10, bytes: 2048 }, "DraglineCompileFull/tiny": { ns: 9, bytes: 3072 }, "WazeroCompile/tiny": { ns: 12, bytes: 4096 },
      "Instantiate/tiny": { ns: 10 }, "DraglineInstantiate/tiny": { ns: 9 }, "WazeroInstantiate/tiny": { ns: 12 },
      "Exec/tiny.add": { ns: 3 }, "DraglineExec/tiny.add": { ns: 2 }, "WazeroExec/tiny.add": { ns: 4 },
    };
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
    await writeFile(amd64, JSON.stringify({ goos: "linux", goarch: "amd64", metrics }));
    await writeFile(arm64, JSON.stringify({ goos: "darwin", goarch: "arm64", metrics }));
    await writeFile(generalAMD64, JSON.stringify({ ...general, goos: "linux" }));
    await writeFile(generalARM64, JSON.stringify(general));
    await writeFile(rssAMD64, JSON.stringify({ version: 1, samples: [
      ...[["railshot-native", 100, 140], ["dragline-native", 110, 170], ["wazero", 120, 220],
        ["cranelift", 140, 300], ["v8", 160, 360], ["wavm", 180, 420]].flatMap(([engine, tiny, large]) => [
        { module: "tiny.wasm", engine, peak_rss_bytes: tiny },
        { module: "large.wasm", engine, peak_rss_bytes: large },
      ]),
    ] }));

    const benchmarkEnv = {
      WAGO_BENCH_JSON_AMD64: amd64,
      WAGO_BENCH_JSON_ARM64: arm64,
      WAGO_GENERAL_JSON_AMD64: generalAMD64,
      WAGO_GENERAL_JSON_ARM64: generalARM64,
      WAGO_COMPILE_RSS_JSON_AMD64: rssAMD64,
    };

    runUpdater(work, benchmarkEnv);
    assertDOMContract(await readFile(index, "utf8"));

    runUpdater(work, { ...benchmarkEnv, WAGO_BENCH_UPDATE_ARCH: "amd64" });
    assertDOMContract(await readFile(index, "utf8"));

    const missingRSS = invokeUpdater(work, {
      ...benchmarkEnv,
      WAGO_COMPILE_RSS_JSON_AMD64: "",
      WAGO_BENCH_UPDATE_ARCH: "amd64",
    });
    assert.notEqual(missingRSS.status, 0);
    assert.match(missingRSS.stderr, /Linux general benchmark requires isolated compileRSS samples/);
  } finally {
    await rm(work, { recursive: true, force: true });
  }
});

function runUpdater(websiteDir, extraEnv = {}) {
  const result = invokeUpdater(websiteDir, extraEnv);
  assert.equal(result.status, 0, result.stderr || result.stdout);
}

function invokeUpdater(websiteDir, extraEnv = {}) {
  return spawnSync(process.execPath, [updater], {
    cwd: root,
    env: {
      ...process.env,
      ...extraEnv,
      WAGO_SITE_NOBUILD: "1",
      WAGO_WEBSITE_DIR: websiteDir,
    },
    encoding: "utf8",
  });
}

function assertDOMContract(html) {
  assert.equal(matches(html, /class="vs__archpanel"/g), 2);
  assert.equal(matches(html, /class="vs__main"/g), 2);
  assert.equal(matches(html, /class="vs__toprow"/g), 2);
  assert.equal(matches(html, /class="vs__specs"/g), 2);
  assert.equal(matches(html, /id="perf-(?:amd64|arm64)-tab-memory"/g), 2);
  assert.equal(matches(html, /data-engine-toggles/g), 2);
  assert.equal(matches(html, /data-engine-toggle=/g), 12);
  assert.equal(matches(html, /data-engine-row/g), 20);
  assert.match(html, /Compile memory growth/);
  assert.match(html, /Mean peak RSS above tiny module · fresh process/);
  assert.doesNotMatch(html, /Compile heap/);
  const rssStart = html.indexOf("Compile memory growth");
  const rssEnd = html.indexOf('<div class="vs__row" data-engine-row>', rssStart);
  const rssRow = html.slice(rssStart, rssEnd);
  for (const engine of ["railshot", "dragline", "wazero", "wasmtime", "v8", "wavm"]) {
    assert.match(rssRow, new RegExp(`data-engine="${engine}"`));
  }
  assert.match(rssRow, /data-engine="railshot"[\s\S]*?data-value="20"/);
  assert.match(html, /Corpus summaries · lower is better/);
  assert.match(html, /End-to-end latency/);
  assert.match(html, /class="vs__side"[^>]*data-arch-toggle/);
  assert.match(html, /class="vs__stage"/);
  assert.match(html, /data-engine-toggle="dragline"/);
  assert.match(html, /data-engine-toggle="wasmtime"/);
  assert.match(html, /data-engine-toggle="v8"/);
  assert.match(html, /data-engine-toggle="wavm"/);
  assert.doesNotMatch(html, /class="[^"]*"[^>]+class="/);
}

function matches(text, pattern) {
  return text.match(pattern)?.length ?? 0;
}
