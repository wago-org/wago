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
    await writeFile(index, `<!doctype html>
<body>
            <!-- ░░░ PERFORMANCE ░░░ -->
            <section id="performance"></section>
            <!-- ░░░ ARCHITECTURE ░░░ -->
            <section id="architecture"></section>
</body>
`);
    const metrics = {
      "Instantiate/tiny": { ns: 10 }, "DraglineInstantiate/tiny": { ns: 9 }, "WazeroInstantiate/tiny": { ns: 12 },
      "Exec/tiny.add": { ns: 3 }, "DraglineExec/tiny.add": { ns: 2 }, "WazeroExec/tiny.add": { ns: 4 },
    };
    const general = {
      compile: [{ runs: [
        { engine: "railshot-native", wall_nanos: 10, peak_rss_bytes: 100 },
        { engine: "dragline-native", wall_nanos: 12, peak_rss_bytes: 110 },
        { engine: "wazero", wall_nanos: 15, peak_rss_bytes: 120 },
        { engine: "cranelift", wall_nanos: 20, peak_rss_bytes: 140 },
      ] }],
      compileRSS: [
        { engine: "railshot-native", peak_rss_bytes: 2048 },
        { engine: "dragline-native", peak_rss_bytes: 3072 },
        { engine: "wazero", peak_rss_bytes: 4096 },
        { engine: "cranelift", peak_rss_bytes: 5120 },
      ],
      wasmtimeRuntime: [
        { stage: "instantiate", module: "tiny.wasm", ns_per_op: 14 },
        { stage: "exec", module: "tiny.wasm", export: "add", ns_per_op: 5 },
      ],
    };
    await writeFile(amd64, JSON.stringify({ goos: "linux", goarch: "amd64", metrics }));
    await writeFile(arm64, JSON.stringify({ goos: "darwin", goarch: "arm64", metrics }));
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
  assert.equal(matches(html, /class="vs__archpanel"/g), 2);
  assert.equal(matches(html, /class="vs__main"/g), 4);
  assert.equal(matches(html, /class="vs__toprow"/g), 4);
  assert.equal(matches(html, /class="vs__specs"/g), 2);
  assert.equal(matches(html, /id="perf-(?:amd64|arm64)-(?:railshot|dragline)-tab-memory"/g), 4);
  assert.equal(matches(html, /class="vs__generalgrid"/g), 4);
  assert.equal(matches(html, /data-bar data-general-bar data-width=/g), 64);
  assert.match(html, /class="vs__side"[^>]*data-arch-toggle/);
  assert.match(html, /class="vs__stage"/);
  assert.equal(matches(html, /vs__glabel">Wasmtime<small>Cranelift<\/small>/g), 16);
  assert.equal(matches(html, /class="vs__gvalue">2 KB<\/span>/g), 4);
  assert.doesNotMatch(html, /wazero's\s+Cranelift|vs__dot--wazero"><\/i>Cranelift/);
  assert.doesNotMatch(html, /class="[^"]*"[^>]+class="/);
}

function matches(text, pattern) {
  return text.match(pattern)?.length ?? 0;
}
