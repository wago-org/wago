#!/usr/bin/env node
// Refresh ../website's hardcoded performance section from benchmark results.
//
// The website intentionally ships static fallback numbers in index.html. This
// script keeps those numbers aligned with bench/out/bench.json when available
// (the same source as the SVG charts), falling back to bench/.bench-run.txt.
// It then runs the website's normal stats sync and build if npm is available.
//
// The section is rendered as a tabbed control (General / Compile / Instantiate /
// Exec): each tab sorts its payloads into grouped wago-vs-wazero rows. Tabs are
// driven by src/tabs.ts on the website side; the markup here is the source of
// truth for which benchmarks land in which tab.

import { access, readFile, writeFile } from "node:fs/promises";
import { constants } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = resolve(__dirname, "..");
const benchJSON = resolve(process.env.WAGO_BENCH_JSON || join(root, "bench", "out", "bench.json"));
const benchFile = resolve(process.env.WAGO_BENCH_IN || join(root, "bench", ".bench-run.txt"));
const websiteDir = resolve(process.env.WAGO_WEBSITE_DIR || join(root, "..", "website"));
const indexPath = join(websiteDir, "index.html");
const requestedUpdateArch = process.env.WAGO_BENCH_UPDATE_ARCH || "";

const benchmarkSets = await loadBenchmarkSets();

// Row/group spec helpers. A spec is pure data (no metric access) — buildRow
// resolves it against `metrics` at render time and drops the row if a key is
// missing, so a corpus rename can't break the whole website build.
const grp = (title) => ({ group: title });
const rs = (label, sub, wagoKey, wazeroKey, winWord = "faster", kind = "ns", forcedDelta = "") =>
  ({ label, sub, wagoKey, wazeroKey, winWord, kind, forcedDelta });
// rsRun is an end-to-end run row: wago and wazero (both JITs) each running one
// real Rust/WASI program to completion.
const rsRun = (label, sub, prog) => ({
  label,
  sub,
  wagoKey: `RunWago/${prog}`,
  wazeroKey: `RunWazero/${prog}`,
  winWord: "faster",
  kind: "ns",
  forcedDelta: "",
});
// dv is a wago-only "front-end at scale" row: the combined Decode+Validate time
// for one real-world binary, with its parse throughput. The bar is sized by the
// binary's byte length, so the visual shows wago's front-end absorbing ever-
// larger real programs. (These same binaries also appear in the Compile tab as a
// full wago-vs-wazero compile race; this tab isolates just the parse throughput.)
const dv = (label, sub, decodeKey, validateKey, bytes) =>
  ({ dv: true, label, sub, decodeKey, validateKey, bytes });

// Each tab sorts its payloads (micro → compute kernel → real-world) into the
// stage it exercises. General is the headline overview shown by default.
const TABS = [
  {
    id: "general",
    label: "General",
    items: [
      rs("Compile latency", "fib_rec module", "Compile/fib_rec", "WazeroCompile/fib_rec"),
      rs("Instantiate latency", "fib_rec startup + mapping", "Instantiate_wago", "Instantiate_wazero"),
      rs("Call overhead", "host → wasm", "ExecCallOverhead_wago", "ExecCallOverhead_wazero"),
      rs("Host roundtrip", "wasm → host → wasm", "ExecHostRoundtrip_wago", "ExecHostRoundtrip_wazero"),
      rs("Exec latency", "fib_rec recursion", "ExecFibRec_wago", "ExecFibRec_wazero"),
      rs("N-body", "leapfrog solar-system integrator", "Exec/nbody.step", "WazeroExec/nbody.step"),
      rs("Ray tracer", "recursive Whitted, depth-4 mirrors", "Exec/raytrace.render", "WazeroExec/raytrace.render"),
      rs("SHA-256", "hash 8 KiB", "Exec/sha256.hashN", "WazeroExec/sha256.hashN"),
      rs("JSON deserialize", "json-as, SWAR", "Exec/json-as.deserializeN", "WazeroExec/json-as.deserializeN"),
      grp("WASI preview 1"),
      rs("WASI compile", "pulldown-cmark module", "WASICompile/markdown.wasm", "WazeroWASICompile/markdown.wasm"),
      rs("WASI instantiate", "compiled module + WASI imports", "WASIInstantiate/markdown.wasm", "WazeroWASIInstantiate/markdown.wasm"),
      rs("WASI run", "pulldown-cmark _start command", "WASIRun/markdown.wasm", "WazeroWASIRun/markdown.wasm"),
    ],
  },
  {
    id: "compile",
    label: "Compile",
    items: [
      grp("Micro modules"),
      rs("tiny", "smallest valid module", "Compile/tiny", "WazeroCompile/tiny"),
      rs("fib_rec", "recursive fib", "Compile/fib_rec", "WazeroCompile/fib_rec"),
      rs("dispatch", "call_indirect table", "Compile/dispatch", "WazeroCompile/dispatch"),
      rs("many_funcs", "thousands of functions", "Compile/many_funcs", "WazeroCompile/many_funcs"),
      grp("Compute kernels"),
      rs("linked_list", "dependent-load chase", "Compile/linked_list", "WazeroCompile/linked_list"),
      rs("memory_tree", "loads + calls", "Compile/memory_tree", "WazeroCompile/memory_tree"),
      rs("sieve", "Eratosthenes", "Compile/sieve", "WazeroCompile/sieve"),
      rs("mandelbrot", "f64 escape-time", "Compile/mandelbrot", "WazeroCompile/mandelbrot"),
      grp("Benchmarks Game (Rust)"),
      rs("nbody", "leapfrog integrator", "Compile/nbody", "WazeroCompile/nbody"),
      rs("spectralnorm", "AᵀA power iteration", "Compile/spectralnorm", "WazeroCompile/spectralnorm"),
      rs("fannkuch", "permutation pancake-flips", "Compile/fannkuch", "WazeroCompile/fannkuch"),
      grp("Crypto & graphics (Rust)"),
      rs("matmul", "64³ f64 multiply-add", "Compile/matmul", "WazeroCompile/matmul"),
      rs("quicksort", "recursive int sort", "Compile/quicksort", "WazeroCompile/quicksort"),
      rs("crc32", "table-driven checksum", "Compile/crc32", "WazeroCompile/crc32"),
      rs("sha256", "SHA-256 hash", "Compile/sha256", "WazeroCompile/sha256"),
      rs("raytrace", "recursive ray tracer", "Compile/raytrace", "WazeroCompile/raytrace"),
      grp("Real-world (AssemblyScript)"),
      rs("json-as", "JSON SWAR", "Compile/json-as", "WazeroCompile/json-as"),
      rs("blake-as", "BLAKE3 SWAR", "Compile/blake-as", "WazeroCompile/blake-as"),
      rs("utf-as", "UTF SWAR transcode", "Compile/utf-as", "WazeroCompile/utf-as"),
      // Real-world interpreters/engines. These carry WASI/host imports so they
      // can't yet be executed here, but the backend compiles them — so this is a
      // like-for-like FULL-compile race (decode + validate + codegen) vs wazero's
      // CompileModule. wago's CompileFull is the matching whole-pipeline metric.
      grp("Real-world programs — full compile: decode + validate + codegen"),
      rs("Lua 5.4", "interpreter · 270 KB", "CompileFull/lua", "WazeroCompile/lua"),
      rs("SQLite 3.46", "database engine · 920 KB", "CompileFull/sqlite3", "WazeroCompile/sqlite3"),
      rs("esbuild", "Go bundler · 12 MB", "CompileFull/esbuild", "WazeroCompile/esbuild"),
      rs("Ruby 3.3", "interpreter · 16 MB, 17k funcs", "CompileFull/ruby", "WazeroCompile/ruby"),
    ],
  },
  {
    id: "instantiate",
    label: "Instantiate",
    items: [
      rs("Cold start", "fib_rec startup + mapping", "Instantiate_wago", "Instantiate_wazero"),
      rs("Heap footprint", "bytes allocated", "Instantiate_wago", "Instantiate_wazero", "leaner", "bytes"),
      rs("Allocations", "objects allocated", "Instantiate_wago", "Instantiate_wazero", "leaner", "count"),
      // Warm instantiate of large real programs (compile once, fresh instance per
      // request — the serving path). wago reuses the compiled code + mapping; these
      // are the same programs as the Compile/Exec "runs end-to-end" groups.
      grp("Large real programs — warm instantiate (Rust / WASI)"),
      rs("markdown", "pulldown-cmark · 320 KB", "InstBigWago/markdown", "InstBigWazero/markdown"),
      rs("serde_json", "serde_json · 96 KB", "InstBigWago/jsonproc", "InstBigWazero/jsonproc"),
      rs("blake3", "blake3 · 57 KB", "InstBigWago/blake3sum", "InstBigWazero/blake3sum"),
      rs("base64", "base64 · 64 KB", "InstBigWago/base64x", "InstBigWazero/base64x"),
      rs("CRC-32", "crc · 51 KB", "InstBigWago/crcsum", "InstBigWazero/crcsum"),
      rs("rhai", "scripting engine · 2.4 MB", "InstBigWago/script", "InstBigWazero/script"),
      rs("regex", "regex engine · 1.2 MB", "InstBigWago/regexmatch", "InstBigWazero/regexmatch"),
    ],
  },
  {
    id: "exec",
    label: "Exec",
    items: [
      grp("Micro ops"),
      rs("Call overhead", "host → wasm", "ExecCallOverhead_wago", "ExecCallOverhead_wazero"),
      rs("Host roundtrip", "wasm → host → wasm (sync host import)", "ExecHostRoundtrip_wago", "ExecHostRoundtrip_wazero"),
      rs("Iterative fib", "fib_iter loop", "ExecFibLoop_wago", "ExecFibLoop_wazero"),
      rs("Recursive fib", "fib_rec", "ExecFibRec_wago", "ExecFibRec_wazero"),
      rs("Dispatch", "call_indirect apply", "Exec/dispatch.apply", "WazeroExec/dispatch.apply"),
      grp("Compute kernels"),
      rs("Linked list", "dependent-load chase", "Exec/linked_list.sum", "WazeroExec/linked_list.sum"),
      rs("Recursive tree", "memory_tree, loads + calls", "Exec/memory_tree.run", "WazeroExec/memory_tree.run"),
      rs("Sieve", "Eratosthenes", "Exec/sieve.count", "WazeroExec/sieve.count"),
      rs("Mandelbrot", "f64 escape-time", "Exec/mandelbrot.render", "WazeroExec/mandelbrot.render"),
      grp("Benchmarks Game (Rust)"),
      rs("N-body", "leapfrog solar-system integrator", "Exec/nbody.step", "WazeroExec/nbody.step"),
      rs("Spectral norm", "AᵀA power iteration + div", "Exec/spectralnorm.run", "WazeroExec/spectralnorm.run"),
      rs("Fannkuch-redux", "permutation pancake-flips", "Exec/fannkuch.run", "WazeroExec/fannkuch.run"),
      grp("Crypto & graphics (Rust)"),
      rs("Matrix multiply", "64³ f64 multiply-add", "Exec/matmul.run", "WazeroExec/matmul.run"),
      rs("Quicksort", "recursive int sort", "Exec/quicksort.sortN", "WazeroExec/quicksort.sortN"),
      rs("CRC-32", "table-driven checksum", "Exec/crc32.hashN", "WazeroExec/crc32.hashN"),
      rs("SHA-256", "64-round hash, 8 KiB", "Exec/sha256.hashN", "WazeroExec/sha256.hashN"),
      rs("Ray tracer", "recursive Whitted, depth-4 mirrors", "Exec/raytrace.render", "WazeroExec/raytrace.render"),
      grp("Real-world (AssemblyScript)"),
      rs("JSON serialize", "json-as, SWAR", "Exec/json-as.serializeN", "WazeroExec/json-as.serializeN"),
      rs("JSON deserialize", "json-as, SWAR", "Exec/json-as.deserializeN", "WazeroExec/json-as.deserializeN"),
      rs("BLAKE3 hash", "blake-as, SWAR", "Exec/blake-as.hashN", "WazeroExec/blake-as.hashN"),
      rs("UTF transcode", "utf-as, SWAR", "Exec/utf-as.convertN", "WazeroExec/utf-as.convertN"),
      grp("AssemblyScript SIMD"),
      rs("JSON serialize", "json-as SIMD", "Exec/json-as-simd.serializeN", "WazeroExec/json-as-simd.serializeN"),
      rs("JSON deserialize", "json-as SIMD", "Exec/json-as-simd.deserializeN", "WazeroExec/json-as-simd.deserializeN"),
      rs("BLAKE3 hash", "blake-as SIMD, 4 KiB", "Exec/blake-as-simd.hashN", "WazeroExec/blake-as-simd.hashN"),
      rs("UTF transcode", "utf-as SIMD, mixed text", "Exec/utf-as-simd.convertN", "WazeroExec/utf-as-simd.convertN"),
      // Real database engine: a real in-memory SQLite query (aggregate table
      // scan) driven through the C API — the same 920 KB engine the Compile tab
      // races, now actually executing on wago.
      grp("Real-world engine (C / SQLite)"),
      rs("SQLite query", "in-memory aggregate scan, 5k rows", "SqliteQueryWago", "SqliteQueryWazero"),
      // Real Rust programs run end-to-end (compile + instantiate + execute). Their
      // whole workload happens in _start, so this whole-program run — not a
      // repeatable export call — is how they execute; wago's fast compile +
      // execution win the run. Same programs as the Compile tab's "runs end-to-end"
      // group; verified by src/wago TestWASIApps.
      grp("Real programs run end-to-end — compile + instantiate + execute (wago · wazero)"),
      rsRun("markdown", "pulldown-cmark render", "markdown"),
      rsRun("serde_json", "parse + aggregate + reserialize", "jsonproc"),
      rsRun("blake3", "BLAKE3 hash", "blake3sum"),
      rsRun("base64", "encode + decode roundtrip", "base64x"),
      rsRun("CRC-32", "crc crate checksum", "crcsum"),
      rsRun("rhai", "run a script (scripting engine)", "script"),
      rsRun("regex", "compile pattern + count matches", "regexmatch"),
    ],
  },
];

const html = await readFile(indexPath, "utf8");
const updateArch = requestedUpdateArch || (
  benchmarkSets.length === 1 &&
  benchmarkSets[0].arch &&
  html.includes(`id="arch-panel-${benchmarkSets[0].arch}"`)
    ? benchmarkSets[0].arch
    : ""
);
const perfAnchor = "            <!-- ░░░ PERFORMANCE ░░░ -->";
const archAnchor = "            <!-- ░░░ ARCHITECTURE ░░░ -->";
const perfStart = html.indexOf(perfAnchor);
const archStart = html.indexOf(archAnchor, perfStart + perfAnchor.length);
if (perfStart < 0 || archStart < 0) {
  throw new Error("could not find website performance section to replace");
}
let updated;
if (updateArch) {
  const set = benchmarkSets.find((candidate) => candidate.arch === updateArch);
  if (!set) {
    throw new Error(`benchmark data does not contain requested architecture ${updateArch}`);
  }
  updated = replaceDivByID(
    html,
    `arch-panel-${updateArch}`,
    renderExistingArchitecture(TABS, set),
  );
  updated = replacePerformanceFoot(updated);
} else {
  const section = renderSection(TABS, benchmarkSets);
  updated = `${html.slice(0, perfStart)}${perfAnchor}\n${section}${html.slice(archStart)}`;
}

await writeFile(indexPath, updated);
console.log(`wago: updated website performance numbers${updateArch ? ` for ${updateArch}` : ""} from ${benchmarkSets.map((s) => s.source).join(", ")}`);

if (!process.env.WAGO_SITE_NOBUILD && (await exists(join(websiteDir, "package.json")))) {
  run("npm", ["run", "sync"], websiteDir);
  run("npm", ["run", "build"], websiteDir);
}

async function loadBenchmarkSets() {
  const amd64 = resolve(process.env.WAGO_BENCH_JSON_AMD64 || join(root, "bench", "out", "amd64", "bench.json"));
  const arm64 = resolve(process.env.WAGO_BENCH_JSON_ARM64 || join(root, "bench", "out", "arm64", "bench.json"));
  if ((await exists(amd64)) && (await exists(arm64))) {
    return [await loadRunMetrics(amd64, "amd64"), await loadRunMetrics(arm64, "arm64")];
  }
  if (await exists(benchJSON)) {
    return [await loadRunMetrics(benchJSON)];
  }
  const benchText = await readFile(benchFile, "utf8");
  const arch = /^goarch:\s+(\S+)/m.exec(benchText)?.[1] || "";
  return [{ metrics: parseBench(benchText), source: benchFile, arch }];
}

async function loadRunMetrics(path, fallbackArch = "") {
  const run = JSON.parse(await readFile(path, "utf8"));
  const metrics = new Map();
  for (const [key, m] of Object.entries(run.metrics ?? {})) {
    metrics.set(key, { ns: Number(m.ns ?? 0), bytes: Number(m.bytes ?? 0), allocs: Number(m.allocs ?? 0) });
  }
  return { metrics, source: path, arch: run.goarch || fallbackArch, goos: run.goos || "" };
}

function parseBench(text) {
  const out = new Map();
  const re = /^Benchmark(\S+?)-\d+\s+\d+\s+([0-9.]+)\s+ns\/op(?:\s+([0-9]+)\s+B\/op)?(?:\s+([0-9]+)\s+allocs\/op)?/gm;
  for (const m of text.matchAll(re)) {
    out.set(m[1], {
      ns: Number(m[2]),
      bytes: m[3] === undefined ? 0 : Number(m[3]),
      allocs: m[4] === undefined ? 0 : Number(m[4]),
    });
  }
  return out;
}

// buildRow resolves a spec against the loaded metrics. Returns null (and warns)
// when either side is missing so the row is skipped rather than crashing.
function buildRow(spec, metrics) {
  const w = metrics.get(spec.wagoKey);
  const z = metrics.get(spec.wazeroKey);
  if (!w || !z) {
    console.warn(`wago: skipping row "${spec.label}" — missing metric ${!w ? spec.wagoKey : spec.wazeroKey}`);
    return null;
  }
  const kind = spec.kind ?? "ns";
  const pick = (m) => (kind === "bytes" ? m.bytes : kind === "count" ? m.allocs : m.ns);
  const fmt = kind === "bytes" ? fmtBytes : kind === "count" ? fmtCount : fmtNs;
  const wv = pick(w);
  const zv = pick(z);
  const max = Math.max(wv, zv, 1);
  const wWins = wv <= zv;
  const same = Math.abs(wv - zv) / Math.max(wv, zv, 1) < 0.03;
  const winWord = spec.winWord ?? "faster";
  const delta =
    spec.forcedDelta ||
    (same ? "same speed" : `${ratio(Math.max(wv, zv) / Math.max(Math.min(wv, zv), 1))}×${wWins ? ` ${winWord}` : " slower"}`);
  return {
    label: spec.label,
    sub: spec.sub,
    wago: fmt(wv),
    wazero: fmt(zv),
    wWidth: barWidth(wv, max),
    zWidth: barWidth(zv, max),
    delta,
    deltaClass: same ? "tie" : wWins ? "win" : "behind",
  };
}

function barWidth(value, max) {
  if (value <= 0) return 4;
  return Math.max(4, Math.round((value / max) * 100));
}

// buildDVRow resolves a wago-only Decode+Validate "scale" row: combined front-end
// time + parse throughput for one real-world binary.
function buildDVRow(spec, metrics) {
  const d = metrics.get(spec.decodeKey);
  const v = metrics.get(spec.validateKey);
  if (!d || !v) {
    console.warn(`wago: skipping scale row "${spec.label}" — missing ${!d ? spec.decodeKey : spec.validateKey}`);
    return null;
  }
  const ns = d.ns + v.ns;
  const mbps = ns > 0 ? spec.bytes / (ns / 1e9) / (1 << 20) : 0;
  return { label: spec.label, sub: spec.sub, time: fmtNs(ns), thru: `${mbps.toFixed(0)} MB/s`, bytes: spec.bytes };
}

// renderDVRow is a single-bar (wago-only) row: the bar is sized by the binary's
// byte length (relative to the largest in the tab), the value is the decode+
// validate time, and the badge is the parse throughput.
function renderDVRow(r, maxBytes, indent) {
  const pad = " ".repeat(indent);
  const w = Math.max(4, Math.round((r.bytes / maxBytes) * 100));
  return `${pad}<div class="vs__row">
${pad}    <div class="vs__meta">
${pad}        <span class="vs__label">${esc(r.label)}</span
${pad}        ><span class="vs__sub">${esc(r.sub)}</span>
${pad}    </div>
${pad}    <div class="vs__bars">
${pad}        <div class="vs__line">
${pad}            <span class="vs__track"
${pad}                ><span
${pad}                    class="vs__fill vs__fill--wago"
${pad}                    data-bar
${pad}                    data-width="${w}"
${pad}                ></span></span
${pad}            ><span class="vs__val vs__val--wago"
${pad}                >${esc(r.time)}</span
${pad}            >
${pad}        </div>
${pad}    </div>
${pad}    <span class="vs__delta vs__delta--win">${esc(r.thru)}</span>
${pad}</div>`;
}

function ratio(v) {
  return v.toFixed(1);
}

function fmtNs(ns) {
  if (ns >= 1e6) return trim(ns / 1e6, ns >= 10e6 ? 1 : 2) + "ms";
  if (ns >= 1e3) return trim(ns / 1e3, ns >= 100e3 ? 0 : 1) + "µs";
  return trim(ns, ns < 10 ? 1 : 1) + "ns";
}

function fmtBytes(bytes) {
  if (bytes >= 1 << 20) return trim(bytes / (1 << 20), 1) + " MB";
  if (bytes >= 1 << 10) return trim(bytes / (1 << 10), bytes >= 100 << 10 ? 0 : 1) + " KB";
  return `${bytes} B`;
}

function fmtCount(n) {
  return String(n);
}

function trim(v, digits) {
  return v.toFixed(digits).replace(/\.0$/, "");
}

function renderSection(tabs, sets) {
  const multiArch = sets.length > 1;
  const archTabs = multiArch
    ? `<div class="vs__tabs" role="tablist" aria-label="Benchmark architecture" data-tabs>
${sets.map((set, i) => `                        <button class="vs__tab" role="tab" id="arch-tab-${set.arch}" aria-controls="arch-panel-${set.arch}" aria-selected="${i === 0 ? "true" : "false"}" tabindex="${i === 0 ? "0" : "-1"}">${esc(archLabel(set))}</button>`).join("\n")}
                    </div>`
    : "";
  const archPanels = sets.map((set, i) => renderArchitecture(tabs, set, i, multiArch)).join("\n");
  const foot = multiArch
    ? "Measured separately on each listed architecture; compare values within an architecture, not across machines."
    : `Measured on ${archLabel(sets[0])} with the single-pass backend; wago vs wazero over the same corpus.`;
  return `            <section id="performance" class="section">
                <div class="eyebrow eyebrow--center">Performance</div>
                <h2 class="section__title">
                    No IR,
                    <span class="section__title-accent">no compromise</span>
                </h2>
                <p class="section__lead">
                    wago compiles straight to native in a single pass: no SSA,
                    no IR, no optimizer, just the novel Valent-Block
                    architecture. It still keeps pace with runtimes that run a
                    full optimizing backend. Every stage, head-to-head with
                    wazero.
                </p>
                <div class="vs">
                    <div class="vs__head">
                        ${archTabs}
                        <div class="vs__legend">
                            <span class="vs__key"><i class="vs__dot vs__dot--wago"></i>wago</span>
                            <span class="vs__key"><i class="vs__dot vs__dot--wazero"></i>wazero</span>
                        </div>
                    </div>
${archPanels}
                </div>
                <p class="vs__foot">
                    ${foot} Numbers shift as the engine evolves — see the
                    <a href="https://github.com/wago-org/wago/tree/main/bench" target="_blank" rel="noopener">benchmark corpus &amp; methodology</a>.
                </p>
            </section>
`;
}

function archLabel(set) {
  return [set.goos, set.arch].filter(Boolean).join("/") || "current host";
}

function renderArchitecture(tabs, set, index, multiArch) {
  const tablist = tabs
    .map(
      (t, i) => `                        <button
                            class="vs__tab"
                            role="tab"
                            id="perf-${set.arch || "host"}-tab-${t.id}"
                            aria-controls="perf-${set.arch || "host"}-panel-${t.id}"
                            aria-selected="${i === 0 ? "true" : "false"}"
                            tabindex="${i === 0 ? "0" : "-1"}"
                        >${esc(t.label)}</button>`
    )
    .join("\n");
  const panels = tabs.map((t, i) => renderPanel(t, i, set.metrics, set.arch || "host")).join("\n");
  const content = `<div class="vs__tabs" role="tablist" aria-label="Benchmark categories" data-tabs>
${tablist}
                    </div>
${panels}`;
  if (!multiArch) return content;
  return `                    <div class="vs__panel" role="tabpanel" id="arch-panel-${set.arch}" aria-labelledby="arch-tab-${set.arch}"${index === 0 ? "" : " hidden"}>
${content}
                    </div>`;
}

// The website may already contain architecture tabs whose other panel came from
// a different machine. Update one measured panel in place so refreshing ARM64
// does not rewrite or round-trip the committed AMD64 reference numbers.
function renderExistingArchitecture(tabs, set) {
  const arch = set.arch || "host";
  const tablist = tabs
    .map(
      (t, i) => `                        <button
                            class="vs__tab"
                            role="tab"
                            id="perf-${arch}-tab-${t.id}"
                            aria-controls="perf-${arch}-panel-${t.id}"
                            aria-selected="${i === 0 ? "true" : "false"}"
                            tabindex="${i === 0 ? "0" : "-1"}"
                        >${esc(t.label)}</button>`,
    )
    .join("\n");
  const panels = tabs.map((tab, i) => renderPanel(tab, i, set.metrics, arch)).join("\n");
  return `                    <div
                        class="vs__panel"
                        role="tabpanel"
                        class="vs__archpanel"
                        id="arch-panel-${arch}"
                        aria-labelledby="arch-tab-${arch}"${arch === "amd64" ? "" : "\n                        hidden"}
                    >
                    <div class="vs__head">
                        <div class="vs__tabs" role="tablist" aria-label="Benchmark categories" data-tabs>
${tablist}
                        </div>
                        <div class="vs__legend">
                            <span class="vs__key"><i class="vs__dot vs__dot--wago"></i>wago</span>
                            <span class="vs__key"><i class="vs__dot vs__dot--wazero"></i>wazero</span>
                        </div>
                    </div>
${panels}
                    </div>`;
}

function replaceDivByID(html, id, replacement) {
  const idAt = html.indexOf(`id="${id}"`);
  if (idAt < 0) throw new Error(`could not find website element ${id}`);
  const start = html.lastIndexOf("<div", idAt);
  const lineStart = html.lastIndexOf("\n", start) + 1;
  const replaceStart = /^\s*$/.test(html.slice(lineStart, start)) ? lineStart : start;
  const tags = /<\/?div\b[^>]*>/g;
  tags.lastIndex = start;
  let depth = 0;
  for (let match; (match = tags.exec(html)); ) {
    depth += match[0].startsWith("</") ? -1 : 1;
    if (depth === 0) {
      return `${html.slice(0, replaceStart)}${replacement}${html.slice(tags.lastIndex)}`;
    }
  }
  throw new Error(`unterminated website element ${id}`);
}

function replacePerformanceFoot(html) {
  const start = html.indexOf('                <p class="vs__foot">');
  const end = html.indexOf("</p>", start);
  if (start < 0 || end < 0) throw new Error("could not find website performance footnote");
  const foot = `                <p class="vs__foot">
                    Measured separately on each listed architecture; compare
                    values within an architecture, not across machines. Numbers
                    shift as the engine evolves — see the
                    <a href="https://github.com/wago-org/wago/tree/main/bench" target="_blank" rel="noopener">benchmark corpus &amp; methodology</a>.
                </p>`;
  return `${html.slice(0, start)}${foot}${html.slice(end + 4)}`;
}

function renderPanel(tab, index, metrics, arch) {
  const dvMax = Math.max(1, ...tab.items.filter((i) => i.dv).map((i) => i.bytes));
  const parts = tab.items.map((item) => {
    if (item.group) return { group: true, html: renderGroup(item.group) };
    if (item.dv) {
      const r = buildDVRow(item, metrics);
      return { group: false, html: r ? renderDVRow(r, dvMax, 24) : null };
    }
    const r = buildRow(item, metrics);
    return { group: false, html: r ? renderRow(r, 24) : null };
  });
  const body = parts
    .filter((part, i) => {
      if (!part.html) return false;
      if (!part.group) return true;
      for (let j = i + 1; j < parts.length && !parts[j].group; j++) {
        if (parts[j].html) return true;
      }
      return false;
    })
    .map((part) => part.html)
    .join("\n");
  return `                    <div
                        class="vs__panel"
                        role="tabpanel"
                        id="perf-${arch}-panel-${tab.id}"
                        aria-labelledby="perf-${arch}-tab-${tab.id}"${index === 0 ? "" : "\n                        hidden"}
                    >
${body}
                    </div>`;
}

function renderGroup(title) {
  return `                        <div class="vs__group">${esc(title)}</div>`;
}

function renderRow(r, indent) {
  const pad = " ".repeat(indent);
  return `${pad}<div class="vs__row">
${pad}    <div class="vs__meta">
${pad}        <span class="vs__label">${esc(r.label)}</span
${pad}        ><span class="vs__sub">${esc(r.sub)}</span>
${pad}    </div>
${pad}    <div class="vs__bars">
${pad}        <div class="vs__line">
${pad}            <span class="vs__track"
${pad}                ><span
${pad}                    class="vs__fill vs__fill--wago"
${pad}                    data-bar
${pad}                    data-width="${r.wWidth}"
${pad}                ></span></span
${pad}            ><span class="vs__val vs__val--wago"
${pad}                >${r.wago}</span
${pad}            >
${pad}        </div>
${pad}        <div class="vs__line">
${pad}            <span class="vs__track"
${pad}                ><span
${pad}                    class="vs__fill vs__fill--wazero"
${pad}                    data-bar
${pad}                    data-width="${r.zWidth}"
${pad}                ></span></span
${pad}            ><span class="vs__val">${r.wazero}</span>
${pad}        </div>
${pad}    </div>
${pad}    <span class="vs__delta vs__delta--${r.deltaClass}"
${pad}        >${r.delta}</span
${pad}    >
${pad}</div>`;
}

function esc(s) {
  return String(s).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

async function exists(path) {
  try {
    await access(path, constants.R_OK);
    return true;
  } catch {
    return false;
  }
}

function run(cmd, args, cwd) {
  const res = spawnSync(cmd, args, { cwd, stdio: "inherit" });
  if (res.error) throw res.error;
  if (res.status !== 0) throw new Error(`${cmd} ${args.join(" ")} failed with exit ${res.status}`);
}
