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
    await writeFile(index, `<!doctype html>
<body>
            <!-- ░░░ PERFORMANCE ░░░ -->
            <section id="performance"></section>
            <!-- ░░░ ARCHITECTURE ░░░ -->
            <section id="architecture"></section>
</body>
`);

    runUpdater(work);
    assertDOMContract(await readFile(index, "utf8"));

    runUpdater(work, { WAGO_BENCH_UPDATE_ARCH: "amd64" });
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
