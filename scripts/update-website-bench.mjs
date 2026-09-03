#!/usr/bin/env node
// Refresh ../website's hardcoded performance section from benchmark results.
//
// The website intentionally ships static fallback numbers in index.html. This
// script keeps those numbers aligned with bench/out/bench.json when available
// (the same source as the SVG charts), falling back to bench/.bench-run.txt.
// It then runs the website's normal stats sync and build if npm is available.
//
// The section is rendered as a tabbed control (General / Compile / Instantiate /
// Memory / Exec): each tab sorts its payloads into grouped wago-vs-wazero rows.
// Tabs are driven by src/tabs.ts on the website side; the markup here is the
// source of truth for which benchmarks land in which tab.

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
const BACKENDS = [
  { id: "railshot", label: "Railshot" },
  { id: "dragline", label: "Dragline" },
];

const benchmarkSets = await loadBenchmarkSets();

// Row/group spec helpers. A spec is pure data (no metric access) — buildRow
// resolves it against `metrics` at render time and drops the row if a key is
// missing, so a corpus rename can't break the whole website build.
const grp = (title) => ({ group: title });
const rs = (label, sub, wagoKey, wazeroKey, winWord = "faster", kind = "ns", forcedDelta = "") =>
  ({ label, sub, wagoKey, wazeroKey, winWord, kind, forcedDelta });
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
      rs("Compile latency", "fib_rec module", "CompileFull/fib_rec", "WazeroCompile/fib_rec"),
      rs("Instantiate latency", "fib_rec startup + mapping", "Instantiate/fib_rec", "WazeroInstantiate/fib_rec"),
      rs("Call overhead", "tiny host → wasm call", "Exec/tiny.add", "WazeroExec/tiny.add"),
      rs("Exec latency", "fib_rec recursion", "Exec/fib_rec.fib", "WazeroExec/fib_rec.fib"),
      rs("N-body", "leapfrog solar-system integrator", "Exec/nbody.step", "WazeroExec/nbody.step"),
      rs("Ray tracer", "recursive Whitted, depth-4 mirrors", "Exec/raytrace.render", "WazeroExec/raytrace.render"),
      rs("SHA-256", "hash 8 KiB", "Exec/sha256.hashN", "WazeroExec/sha256.hashN"),
      rs("JSON deserialize", "json-as, SWAR", "Exec/json-as.deserializeN", "WazeroExec/json-as.deserializeN"),
    ],
  },
  {
    id: "compile",
    label: "Compile",
    items: [
      grp("Micro modules"),
      rs("tiny", "smallest valid module", "CompileFull/tiny", "WazeroCompile/tiny"),
      rs("fib_rec", "recursive fib", "CompileFull/fib_rec", "WazeroCompile/fib_rec"),
      rs("dispatch", "call_indirect table", "CompileFull/dispatch", "WazeroCompile/dispatch"),
      rs("many_funcs", "thousands of functions", "CompileFull/many_funcs", "WazeroCompile/many_funcs"),
      grp("Compute kernels"),
      rs("linked_list", "dependent-load chase", "CompileFull/linked_list", "WazeroCompile/linked_list"),
      rs("memory_tree", "loads + calls", "CompileFull/memory_tree", "WazeroCompile/memory_tree"),
      rs("sieve", "Eratosthenes", "CompileFull/sieve", "WazeroCompile/sieve"),
      rs("mandelbrot", "f64 escape-time", "CompileFull/mandelbrot", "WazeroCompile/mandelbrot"),
      grp("Benchmarks Game (Rust)"),
      rs("nbody", "leapfrog integrator", "CompileFull/nbody", "WazeroCompile/nbody"),
      rs("spectralnorm", "AᵀA power iteration", "CompileFull/spectralnorm", "WazeroCompile/spectralnorm"),
      rs("fannkuch", "permutation pancake-flips", "CompileFull/fannkuch", "WazeroCompile/fannkuch"),
      grp("Crypto & graphics (Rust)"),
      rs("matmul", "64³ f64 multiply-add", "CompileFull/matmul", "WazeroCompile/matmul"),
      rs("quicksort", "recursive int sort", "CompileFull/quicksort", "WazeroCompile/quicksort"),
      rs("crc32", "table-driven checksum", "CompileFull/crc32", "WazeroCompile/crc32"),
      rs("sha256", "SHA-256 hash", "CompileFull/sha256", "WazeroCompile/sha256"),
      rs("raytrace", "recursive ray tracer", "CompileFull/raytrace", "WazeroCompile/raytrace"),
      grp("Real-world (AssemblyScript)"),
      rs("json-as", "JSON SWAR", "CompileFull/json-as", "WazeroCompile/json-as"),
      rs("blake-as", "BLAKE3 SWAR", "CompileFull/blake-as", "WazeroCompile/blake-as"),
      rs("utf-as", "UTF SWAR transcode", "CompileFull/utf-as", "WazeroCompile/utf-as"),
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
      grp("Micro modules"),
      rs("tiny", "smallest valid module", "Instantiate/tiny", "WazeroInstantiate/tiny"),
      rs("fib_rec", "recursive fib", "Instantiate/fib_rec", "WazeroInstantiate/fib_rec"),
      rs("many_funcs", "thousands of functions", "Instantiate/many_funcs", "WazeroInstantiate/many_funcs"),
      grp("Compute kernels"),
      rs("linked_list", "dependent-load chase", "Instantiate/linked_list", "WazeroInstantiate/linked_list"),
      rs("sieve", "Eratosthenes", "Instantiate/sieve", "WazeroInstantiate/sieve"),
      rs("nbody", "leapfrog integrator", "Instantiate/nbody", "WazeroInstantiate/nbody"),
      rs("matmul", "64³ f64 multiply-add", "Instantiate/matmul", "WazeroInstantiate/matmul"),
      rs("raytrace", "recursive ray tracer", "Instantiate/raytrace", "WazeroInstantiate/raytrace"),
      grp("AssemblyScript"),
      rs("json-as", "JSON SWAR", "Instantiate/json-as", "WazeroInstantiate/json-as"),
      rs("blake-as", "BLAKE3 SWAR", "Instantiate/blake-as", "WazeroInstantiate/blake-as"),
      rs("utf-as", "UTF SWAR transcode", "Instantiate/utf-as", "WazeroInstantiate/utf-as"),
    ],
  },
  {
    id: "memory",
    label: "Memory",
    items: [
      grp("Instantiation"),
      rs("fib_rec instance", "bytes allocated per fresh instance", "Instantiate/fib_rec", "WazeroInstantiate/fib_rec", "leaner", "bytes"),
      rs("fib_rec instance", "allocation objects per fresh instance", "Instantiate/fib_rec", "WazeroInstantiate/fib_rec", "leaner", "count"),
      grp("Full compile — allocation bytes"),
      rs("tiny", "smallest module", "CompileFull/tiny", "WazeroCompile/tiny", "leaner", "bytes"),
      rs("memory tree", "calls + linear-memory access", "CompileFull/memory_tree", "WazeroCompile/memory_tree", "leaner", "bytes"),
      rs("json-as", "AssemblyScript JSON", "CompileFull/json-as", "WazeroCompile/json-as", "leaner", "bytes"),
      rs("blake-as", "AssemblyScript BLAKE3", "CompileFull/blake-as", "WazeroCompile/blake-as", "leaner", "bytes"),
      rs("esbuild", "Go bundler · 12 MB", "CompileFull/esbuild", "WazeroCompile/esbuild", "leaner", "bytes"),
      rs("Ruby 3.3", "interpreter · 16 MB", "CompileFull/ruby", "WazeroCompile/ruby", "leaner", "bytes"),
      grp("Full compile — allocation objects"),
      rs("tiny", "smallest module", "CompileFull/tiny", "WazeroCompile/tiny", "leaner", "count"),
      rs("memory tree", "calls + linear-memory access", "CompileFull/memory_tree", "WazeroCompile/memory_tree", "leaner", "count"),
      rs("json-as", "AssemblyScript JSON", "CompileFull/json-as", "WazeroCompile/json-as", "leaner", "count"),
      rs("blake-as", "AssemblyScript BLAKE3", "CompileFull/blake-as", "WazeroCompile/blake-as", "leaner", "count"),
      rs("esbuild", "Go bundler · 12 MB", "CompileFull/esbuild", "WazeroCompile/esbuild", "leaner", "count"),
      rs("Ruby 3.3", "interpreter · 16 MB", "CompileFull/ruby", "WazeroCompile/ruby", "leaner", "count"),
    ],
  },
  {
    id: "exec",
    label: "Exec",
    items: [
      grp("Micro ops"),
      rs("Call overhead", "tiny host → wasm call", "Exec/tiny.add", "WazeroExec/tiny.add"),
      rs("Iterative fib", "fib_iter loop", "Exec/fib_iter.fib", "WazeroExec/fib_iter.fib"),
      rs("Recursive fib", "fib_rec", "Exec/fib_rec.fib", "WazeroExec/fib_rec.fib"),
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
  const arch = run.goarch || fallbackArch;
  const generalPath = resolve(
    arch === "amd64"
      ? process.env.WAGO_GENERAL_JSON_AMD64 || join(root, "bench", "out", "amd64", "general.json")
      : process.env.WAGO_GENERAL_JSON_ARM64 || join(root, "bench", "out", "arm64", "general.json"),
  );
  const generalRaw = (await exists(generalPath)) ? JSON.parse(await readFile(generalPath, "utf8")) : null;
  if (generalRaw?.goarch && generalRaw.goarch !== arch) {
    throw new Error(`general benchmark architecture ${generalRaw.goarch} does not match ${arch}`);
  }
  if (generalRaw?.commit && run.commit && !String(run.commit).startsWith(generalRaw.commit) && !String(generalRaw.commit).startsWith(run.commit)) {
    throw new Error(`general benchmark commit ${generalRaw.commit} does not match ${run.commit}`);
  }
  const general = generalRaw ? buildGeneralSummary(metrics, generalRaw) : null;
  return { metrics, general, source: path, arch, goos: run.goos || "", commit: run.commit || "", cpu: run.cpu || "" };
}

function buildGeneralSummary(metrics, raw) {
  const compileNames = new Map([
    ["railshot-native", "railshot"],
    ["dragline-native", "dragline"],
    ["wazero", "wazero"],
    ["cranelift", "wasmtime"],
  ]);
  const compile = new Map();
  for (const report of raw.compile ?? []) {
    const byEngine = new Map();
    for (const run of report.runs ?? []) {
      const engine = compileNames.get(run.engine);
      if (!engine) continue;
      const values = byEngine.get(engine) ?? { wall: [], rss: [] };
      values.wall.push(Number(run.wall_nanos));
      values.rss.push(Number(run.peak_rss_bytes));
      byEngine.set(engine, values);
    }
    for (const [engine, values] of byEngine) {
      const aggregate = compile.get(engine) ?? { wall: [], rss: [] };
      aggregate.wall.push(median(values.wall));
      aggregate.rss.push(median(values.rss));
      compile.set(engine, aggregate);
    }
  }
  if (Array.isArray(raw.compileRSS)) {
    const rssByEngine = new Map();
    for (const sample of raw.compileRSS) {
      const engine = compileNames.get(sample.engine);
      if (!engine || !(Number(sample.peak_rss_bytes) > 0)) continue;
      const values = rssByEngine.get(engine) ?? [];
      values.push(Number(sample.peak_rss_bytes));
      rssByEngine.set(engine, values);
    }
    for (const engine of compileNames.values()) {
      if (rssByEngine.get(engine)?.length !== raw.compile.length) {
        throw new Error(`general benchmark has incomplete direct RSS samples for ${engine}`);
      }
    }
    for (const [engine, values] of rssByEngine) {
      const aggregate = compile.get(engine);
      if (aggregate) aggregate.rss = values;
    }
  }
  const runtime = raw.wasmtimeRuntime ?? [];
  return [
    ["Compile time", "Fresh-process wall time", "ns", Object.fromEntries(
      [...compile].map(([engine, values]) => [engine, geomean(values.wall)]),
    )],
    ["Compile memory", "Fresh-process peak RSS", "bytes", Object.fromEntries(
      [...compile].map(([engine, values]) => [engine, geomean(values.rss)]),
    )],
    ["Instantiate time", "Runnable corpus", "ns", {
      railshot: metricGeomean(metrics, "Instantiate/"),
      dragline: metricGeomean(metrics, "DraglineInstantiate/"),
      wazero: metricGeomean(metrics, "WazeroInstantiate/"),
      wasmtime: externalRuntimeGeomean(runtime, "instantiate"),
    }],
    ["Execution time", "Runnable corpus", "ns", {
      railshot: metricGeomean(metrics, "Exec/", true),
      dragline: metricGeomean(metrics, "DraglineExec/", true),
      wazero: metricGeomean(metrics, "WazeroExec/", true),
      wasmtime: externalRuntimeGeomean(runtime, "exec", true),
    }],
  ].map(([label, sub, kind, values]) => ({ label, sub, kind, values }));
}

function metricGeomean(metrics, prefix, groupExports = false) {
  const groups = new Map();
  for (const [key, metric] of metrics) {
    if (!key.startsWith(prefix) || !(metric.ns > 0)) continue;
    const tail = key.slice(prefix.length);
    const group = groupExports ? tail.split(".", 1)[0] : tail;
    const values = groups.get(group) ?? [];
    values.push(metric.ns);
    groups.set(group, values);
  }
  return geomean([...groups.values()].map(geomean));
}

function externalRuntimeGeomean(rows, stage, groupExports = false) {
  const samples = new Map();
  for (const row of rows) {
    if (row.stage !== stage || !(Number(row.ns_per_op) > 0)) continue;
    const module = basenameWithoutWasm(row.module);
    const key = groupExports ? `${module}.${row.export}` : module;
    const values = samples.get(key) ?? [];
    values.push(Number(row.ns_per_op));
    samples.set(key, values);
  }
  const entries = new Map([...samples].map(([key, values]) => [key, median(values)]));
  if (!groupExports) return geomean([...entries.values()]);
  const modules = new Map();
  for (const [key, value] of entries) {
    const module = key.split(".", 1)[0];
    const values = modules.get(module) ?? [];
    values.push(value);
    modules.set(module, values);
  }
  return geomean([...modules.values()].map(geomean));
}

function basenameWithoutWasm(path) {
  return String(path).replaceAll("\\", "/").split("/").at(-1).replace(/\.wasm$/, "");
}

function median(values) {
  if (values.length === 0) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

function geomean(values) {
  const positive = values.filter((value) => value > 0);
  return positive.length ? Math.exp(positive.reduce((sum, value) => sum + Math.log(value), 0) / positive.length) : 0;
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
function backendMetricKey(key, backend) {
  if (backend === "railshot") return key;
  for (const [stage, replacement] of [
    ["CompileFull/", "DraglineCompileFull/"],
    ["Instantiate/", "DraglineInstantiate/"],
    ["Exec/", "DraglineExec/"],
  ]) {
    if (key.startsWith(stage)) return replacement + key.slice(stage.length);
  }
  // Decode and validation are shared frontend work. Backend-specific tabs omit
  // fixed microbenchmarks and runtime integrations without a paired Dragline row.
  if (key.startsWith("Decode/") || key.startsWith("Validate/")) return key;
  return "";
}

function buildRow(spec, metrics, backend) {
  const wagoKey = backendMetricKey(spec.wagoKey, backend);
  const w = wagoKey ? metrics.get(wagoKey) : null;
  const z = metrics.get(spec.wazeroKey);
  if (!w || !z) {
    console.warn(`wago: skipping ${backend} row "${spec.label}" — missing metric ${!w ? wagoKey || spec.wagoKey : spec.wazeroKey}`);
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
  const archTabs = sets.map((set, i) => `                            <button class="vs__archbtn" role="tab" id="arch-tab-${set.arch}" aria-controls="arch-panel-${set.arch}" aria-selected="${i === 0 ? "true" : "false"}" tabindex="${i === 0 ? "0" : "-1"}">${esc(set.arch || "host")}</button>`).join("\n");
  const archPanels = sets.map((set, i) => renderArchitecture(tabs, set, i)).join("\n");
  const foot = multiArch
    ? "Measured separately on each listed architecture; compare values within an architecture, not across machines."
    : `Measured on ${archLabel(sets[0])}; each selected Wago backend is compared with wazero over the same corpus.`;
  const out = `            <section id="performance" class="section">
                <div class="eyebrow eyebrow--center">Performance</div>
                <h2 class="section__title">
                    Two compilers,
                    <span class="section__title-accent">one corpus</span>
                </h2>
                <p class="section__lead">
                    Compare Railshot's direct single-pass compiler and the
                    experimental optimizing Dragline backend against wazero's
                    compiler backend. Every published row uses the same
                    workload on the selected architecture.
                </p>
                <div class="vs">
                    <div class="vs__body">
                        <div class="vs__side" role="tablist" aria-label="Benchmark platform" data-arch-toggle>
${archTabs}
                        </div>
                        <div class="vs__stage">
${archPanels}
                        </div>
                    </div>
                </div>
                <p class="vs__foot">
                    ${foot} Rows appear only when the selected backend completes
                    the workload. Numbers shift as the engine evolves — see the
                    <a href="https://github.com/wago-org/wago/tree/main/bench" target="_blank" rel="noopener">benchmark corpus &amp; methodology</a>.
                </p>
            </section>
`;
  for (const marker of ["vs__body", "vs__side", "data-arch-toggle", "vs__stage"]) {
    if (!out.includes(marker)) {
      throw new Error(`benchmark section renderer lost required ${marker} markup`);
    }
  }
  return out;
}

function archLabel(set) {
  return [set.goos, set.arch].filter(Boolean).join("/") || "current host";
}

function renderArchitecture(tabs, set, index) {
  return renderArchitecturePanel(tabs, set, index);
}

function renderBackend(tabs, set, backend, index) {
  const arch = set.arch || "host";
  const tablist = tabs
    .map(
      (t, i) => `                            <button
                            class="vs__tab"
                            role="tab"
                            id="perf-${arch}-${backend.id}-tab-${t.id}"
                            aria-controls="perf-${arch}-${backend.id}-panel-${t.id}"
                            aria-selected="${i === 0 ? "true" : "false"}"
                            tabindex="${i === 0 ? "0" : "-1"}"
                        >${esc(t.label)}</button>`
    )
    .join("\n");
  const panels = tabs.map((t, i) => renderPanel(t, i, set, arch, backend.id)).join("\n");
  return `                        <div
                            class="vs__backendpanel"
                            role="tabpanel"
                            id="backend-panel-${arch}-${backend.id}"
                            aria-labelledby="backend-tab-${arch}-${backend.id}"${index === 0 ? "" : "\n                            hidden"}
                        >
                            <div class="vs__main">
                                <div class="vs__toprow">
                                    <div class="vs__tabs" role="tablist" aria-label="Benchmark categories" data-tabs>
${tablist}
                                    </div>
                                    <div class="vs__legend">
                                        <span class="vs__key"><i class="vs__dot vs__dot--wago"></i>${backend.label}</span>
                                        <span class="vs__key"><i class="vs__dot vs__dot--wazero"></i>wazero</span>
                                    </div>
                                </div>
${panels}
                            </div>
                        </div>`;
}

// The website may already contain architecture tabs whose other panel came from
// a different machine. Update one measured panel in place so refreshing ARM64
// does not rewrite or round-trip the committed AMD64 reference numbers.
function renderExistingArchitecture(tabs, set) {
  return renderArchitecturePanel(tabs, set, (set.arch || "host") === "amd64" ? 0 : 1);
}

// Keep the platform rail, category tabs, and capped row viewport separate. The
// website CSS and tabs controller target this exact structure; flattening it
// makes long Compile/Exec tabs expand the whole card after every regeneration.
function renderArchitecturePanel(tabs, set, index) {
  const arch = set.arch || "host";
  const spec = [set.goos, set.arch, set.cpu, set.commit ? `wago ${set.commit}` : ""].filter(Boolean).join(" · ");
  const backendTabs = BACKENDS.map((backend, i) => `                            <button class="vs__archbtn" role="tab" id="backend-tab-${arch}-${backend.id}" aria-controls="backend-panel-${arch}-${backend.id}" aria-selected="${i === 0 ? "true" : "false"}" tabindex="${i === 0 ? "0" : "-1"}">${backend.label}</button>`).join("\n");
  const backendPanels = BACKENDS.map((backend, i) => renderBackend(tabs, set, backend, i)).join("\n");
  const out = `                    <div
                        class="vs__archpanel"
                        role="tabpanel"
                        id="arch-panel-${arch}"
                        aria-labelledby="arch-tab-${arch}"${index === 0 ? "" : "\n                        hidden"}
                    >
                        <div class="vs__backendstage">
${backendPanels}
                        </div>
                        <div class="vs__specs">${esc(spec)}</div>
                        <div class="vs__side vs__side--backend" role="tablist" aria-label="Compiler backend" data-backend-toggle>
${backendTabs}
                        </div>
                    </div>`;
  for (const marker of ["vs__archpanel", "vs__backendstage", "data-backend-toggle", "vs__main", "vs__toprow", "vs__tabs", "vs__specs"]) {
    if (!out.includes(marker)) {
      throw new Error(`benchmark architecture renderer lost required ${marker} markup`);
    }
  }
  return out;
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
                    values within an architecture, not across machines. Rows
                    appear only when the selected backend completes the workload. Numbers
                    shift as the engine evolves — see the
                    <a href="https://github.com/wago-org/wago/tree/main/bench" target="_blank" rel="noopener">benchmark corpus &amp; methodology</a>.
                </p>`;
  return `${html.slice(0, start)}${foot}${html.slice(end + 4)}`;
}

function renderPanel(tab, index, set, arch, backend) {
  if (tab.id === "general" && set.general) {
    return renderGeneralPanel(tab, index, set.general, arch, backend);
  }
  const metrics = set.metrics;
  const dvMax = Math.max(1, ...tab.items.filter((i) => i.dv).map((i) => i.bytes));
  const parts = tab.items.map((item) => {
    if (item.group) return { group: true, html: renderGroup(item.group) };
    if (item.dv) {
      const r = buildDVRow(item, metrics);
      return { group: false, html: r ? renderDVRow(r, dvMax, 24) : null };
    }
    const r = buildRow(item, metrics, backend);
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
                        id="perf-${arch}-${backend}-panel-${tab.id}"
                        aria-labelledby="perf-${arch}-${backend}-tab-${tab.id}"${index === 0 ? "" : "\n                        hidden"}
                    >
${body}
                    </div>`;
}

function renderGeneralPanel(tab, index, summary, arch, backend) {
  const engines = [
    { id: "railshot", label: "Railshot" },
    { id: "dragline", label: "Dragline" },
    { id: "wazero", label: "wazero" },
    { id: "wasmtime", label: "Wasmtime", sub: "Cranelift" },
  ];
  const cards = summary.map((metric) => {
    const max = Math.max(1, ...engines.map((engine) => Number(metric.values[engine.id] ?? 0)));
    const format = metric.kind === "bytes" ? fmtBytes : fmtNs;
    const bars = engines.map((engine) => {
      const value = Number(metric.values[engine.id] ?? 0);
      const height = value > 0 ? Math.max(5, Math.round(value / max * 100)) : 0;
      return `                                <div class="vs__vitem${engine.id === backend ? " vs__vitem--selected" : ""}">
                                    <span class="vs__vvalue">${value > 0 ? format(value) : "—"}</span>
                                    <span class="vs__vtrack"><span class="vs__vfill vs__vfill--${engine.id}" data-vbar data-height="${height}"></span></span>
                                    <span class="vs__vlabel">${engine.label}${engine.sub ? `<small>${engine.sub}</small>` : ""}</span>
                                </div>`;
    }).join("\n");
    return `                        <article class="vs__metriccard">
                            <div class="vs__metrichead"><strong>${esc(metric.label)}</strong><span>${esc(metric.sub)}</span></div>
                            <div class="vs__vbars">
${bars}
                            </div>
                        </article>`;
  }).join("\n");
  return `                    <div
                        class="vs__panel vs__panel--general"
                        role="tabpanel"
                        id="perf-${arch}-${backend}-panel-${tab.id}"
                        aria-labelledby="perf-${arch}-${backend}-tab-${tab.id}"${index === 0 ? "" : "\n                        hidden"}
                    >
                        <div class="vs__generalkicker">Corpus geometric mean · lower is better</div>
                        <div class="vs__generalgrid">
${cards}
                        </div>
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
