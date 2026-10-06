import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

const root = resolve(import.meta.dirname, "..");
const updater = join(root, "scripts", "update-website-startup.mjs");
const commit = "363fadfbf9bf0d0a8e9b995ca1c5a498ad152fc8";

test("renders matched end-to-end architecture panels with standardized engines", () => {
  const website = mkdtempSync(join(tmpdir(), "wago-startup-site-"));
  const arm64 = join(website, "startup-arm64.json");
  const amd64 = join(website, "startup-amd64.json");
  writeFileSync(
    join(website, "index.html"),
    "            <!-- ░░░ END-TO-END LATENCY ░░░ -->\nold\n            <!-- ░░░ PERFORMANCE ░░░ -->\n",
  );
  writeFileSync(arm64, JSON.stringify(dataset("arm64", "Apple M4 Max, darwin/arm64")));
  writeFileSync(amd64, JSON.stringify(dataset("amd64", "AMD Ryzen 7 7800X3D, linux/amd64, CPU 7 pinned")));

  execFileSync(process.execPath, [updater], {
    env: {
      ...process.env,
      WAGO_SITE_NOBUILD: "1",
      WAGO_WEBSITE_DIR: website,
      WAGO_STARTUP_JSON_ARM64: arm64,
      WAGO_STARTUP_JSON_AMD64: amd64,
    },
  });

  const html = readFileSync(join(website, "index.html"), "utf8");
  assert.equal(matches(html, /data-startup-arch-target=/g), 2);
  assert.equal(matches(html, /data-startup-arch-panel=/g), 2);
  assert.match(html, />wago<span class="rank__mode">single-pass<\/span>/);
  assert.match(html, />wazy<span class="rank__tag">compiler<\/span>/);
  assert.match(html, />wasmtime<span class="rank__tag">cranelift<\/span>/);
  assert.match(html, />v8<span class="rank__tag">turboshaft<\/span>/);
  assert.match(html, />wasm3<span class="rank__tag">interpreter<\/span>/);
  assert.match(html, />wasmi<span class="rank__tag">interpreter<\/span>/);
  assert.match(html, />wavm<span class="rank__tag">llvm<\/span>/);
  assert.doesNotMatch(html, /wasmer/i);
  assert.match(html, /Apple M4 Max, darwin\/arm64/);
  assert.match(html, /AMD Ryzen 7 7800X3D, linux\/amd64, CPU 7 pinned/);
});

test("rejects architecture captures from different source commits", () => {
  const website = mkdtempSync(join(tmpdir(), "wago-startup-site-"));
  const arm64 = join(website, "startup-arm64.json");
  const amd64 = join(website, "startup-amd64.json");
  writeFileSync(join(website, "index.html"), "            <!-- ░░░ END-TO-END LATENCY ░░░ -->\nold\n            <!-- ░░░ PERFORMANCE ░░░ -->\n");
  writeFileSync(arm64, JSON.stringify(dataset("arm64", "arm")));
  writeFileSync(amd64, JSON.stringify({ ...dataset("amd64", "amd"), sourceCommit: "0".repeat(40) }));

  assert.throws(
    () => execFileSync(process.execPath, [updater], {
      env: { ...process.env, WAGO_SITE_NOBUILD: "1", WAGO_WEBSITE_DIR: website, WAGO_STARTUP_JSON_ARM64: arm64, WAGO_STARTUP_JSON_AMD64: amd64 },
      stdio: "pipe",
    }),
    /Command failed/,
  );
});

function dataset(architecture, machine) {
  const runtimes = {
    wago: { label: "wago", tag: "single-pass" },
    wazy: { label: "wazy", tag: "compiler" },
    wazero: { label: "wazero", tag: "compiler" },
    wasmtime: { label: "wasmtime", tag: "cranelift" },
    v8: { label: "v8", tag: "turboshaft" },
    wasm3: { label: "wasm3", tag: "interpreter" },
    wasmi: { label: "wasmi", tag: "interpreter" },
    wavm: { label: "wavm", tag: "llvm" },
  };
  return {
    generated: "2026-09-10",
    sourceCommit: commit,
    machine,
    method: "hyperfine test fixture",
    unit: "ms",
    architecture,
    runtimes,
    workloads: [{ id: "fib", label: "fib", desc: "recursive Fibonacci", results: { wago: 1, wazy: 1.5, wazero: 2, wasmtime: 3, v8: 4, wasm3: 5, wasmi: 6, wavm: 7 } }],
  };
}

function matches(text, pattern) {
  return text.match(pattern)?.length ?? 0;
}
