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
    await writeFile(index, `<!doctype html>
<body>
            <!-- ░░░ PERFORMANCE ░░░ -->
            <section id="performance"></section>
            <!-- ░░░ ARCHITECTURE ░░░ -->
            <section id="architecture"></section>
</body>
`);
    await writeFile(amd64, JSON.stringify({ goos: "linux", goarch: "amd64", metrics: {} }));
    await writeFile(arm64, JSON.stringify({ goos: "darwin", goarch: "arm64", metrics: {} }));

    const benchmarkEnv = {
      WAGO_BENCH_JSON_AMD64: amd64,
      WAGO_BENCH_JSON_ARM64: arm64,
    };

    runUpdater(work, benchmarkEnv);
    assertDOMContract(await readFile(index, "utf8"));

    runUpdater(work, { ...benchmarkEnv, WAGO_BENCH_UPDATE_ARCH: "amd64" });
    assertDOMContract(await readFile(index, "utf8"));
  } finally {
    await rm(work, { recursive: true, force: true });
  }
});

test("benchmark regeneration publishes paired semantic corpus rows", async () => {
  const work = await mkdtemp(join(tmpdir(), "wago-bench-site-semantic-"));
  try {
    const index = join(work, "index.html");
    const amd64 = join(work, "amd64.json");
    const arm64 = join(work, "arm64.json");
    await writeFile(index, `<!doctype html>
<body>
            <!-- ░░░ PERFORMANCE ░░░ -->
            <section id="performance"></section>
            <!-- ░░░ ARCHITECTURE ░░░ -->
            <section id="architecture"></section>
</body>
`);
    const metrics = {};
    for (const name of ["coremark", "blake3", "qoi", "lz4", "zlib", "zstd"]) {
      metrics[`CompileFull/${name}`] = { ns: 100, bytes: 10, allocs: 1 };
      metrics[`WazeroCompile/${name}`] = { ns: 200, bytes: 20, allocs: 2 };
    }
    await writeFile(amd64, JSON.stringify({ goos: "linux", goarch: "amd64", metrics }));
    await writeFile(arm64, JSON.stringify({ goos: "darwin", goarch: "arm64", metrics }));

    runUpdater(work, {
      WAGO_BENCH_JSON_AMD64: amd64,
      WAGO_BENCH_JSON_ARM64: arm64,
    });
    const html = await readFile(index, "utf8");
    assert.equal(matches(html, /Semantic corpus — full compile/g), 2);
    for (const label of ["CoreMark", "BLAKE3", "QOI", "LZ4", "zlib", "Zstandard"]) {
      assert.equal(matches(html, new RegExp(`vs__label">${label}<`, "g")), 2);
    }
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
  assert.equal(matches(html, /class="vs__main"/g), 2);
  assert.equal(matches(html, /class="vs__toprow"/g), 2);
  assert.equal(matches(html, /class="vs__specs"/g), 2);
  assert.equal(matches(html, /id="perf-(?:amd64|arm64)-tab-memory"/g), 2);
  assert.match(html, /class="vs__side"[^>]*data-arch-toggle/);
  assert.match(html, /class="vs__stage"/);
  assert.doesNotMatch(html, /class="[^"]*"[^>]+class="/);
}

function matches(text, pattern) {
  return text.match(pattern)?.length ?? 0;
}
