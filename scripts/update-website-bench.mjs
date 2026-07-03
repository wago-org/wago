#!/usr/bin/env node
// Refresh ../website's hardcoded performance section from benchmark results.
//
// The website intentionally ships static fallback numbers in index.html. This
// script keeps those numbers aligned with bench/out/bench.json when available
// (the same source as the SVG charts), falling back to bench/.bench-run.txt.
// It then runs the website's normal stats sync and build if npm is available.

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

const { metrics, source } = await loadMetrics();

const rows = [
  row("Compile latency", "fib_rec module", "Compile/fib_rec", "WazeroCompile/fib_rec", "faster"),
  row("Compile memory", "fib_rec heap bytes", "Compile/fib_rec", "WazeroCompile/fib_rec", "leaner", "bytes"),
  row("Instantiate latency", "fib_rec startup + mapping", "Instantiate_wago", "Instantiate_wazero", "faster"),
  row("Call overhead", "host \u2192 wasm", "ExecCallOverhead_wago", "ExecCallOverhead_wazero", "faster"),
  row("Exec latency", "fib_rec", "ExecFibRec_wago", "ExecFibRec_wazero", "faster"),
  row("Exec memory", "json-as deserialize", "Exec/json-as.deserializeN", "WazeroExec/json-as.deserializeN", "leaner", "bytes", "0 vs 1 alloc"),
  row("Exec latency", "json-as deserialize", "Exec/json-as.deserializeN", "WazeroExec/json-as.deserializeN", "faster"),
];

const moreRows = [
  row("Iterative fib", "fib_iter loop", "ExecFibLoop_wago", "ExecFibLoop_wazero", "faster"),
  row("Recursive tree", "memory_tree, loads + calls", "Exec/memory_tree.run", "WazeroExec/memory_tree.run", "faster"),
  row("JSON serialize", "json-as, SWAR", "Exec/json-as.serializeN", "WazeroExec/json-as.serializeN", "faster"),
  row("BLAKE3 hash", "blake-as, SWAR", "Exec/blake-as.hashN", "WazeroExec/blake-as.hashN", "faster"),
  row("UTF transcode", "utf-as, SWAR", "Exec/utf-as.convertN", "WazeroExec/utf-as.convertN", "faster"),
];

const html = await readFile(indexPath, "utf8");
const section = renderSection(rows, moreRows);
const perfAnchor = "            <!-- \u2591\u2591\u2591 PERFORMANCE \u2591\u2591\u2591 -->";
const archAnchor = "            <!-- \u2591\u2591\u2591 ARCHITECTURE \u2591\u2591\u2591 -->";
const perfStart = html.indexOf(perfAnchor);
const archStart = html.indexOf(archAnchor, perfStart + perfAnchor.length);
if (perfStart < 0 || archStart < 0) {
  throw new Error("could not find website performance section to replace");
}
const updated = `${html.slice(0, perfStart)}${perfAnchor}\n${section}${html.slice(archStart)}`;

await writeFile(indexPath, updated);
console.log(`wago: updated website performance numbers from ${source}`);

if (await exists(join(websiteDir, "package.json"))) {
  run("npm", ["run", "sync"], websiteDir);
  run("npm", ["run", "build"], websiteDir);
}

async function loadMetrics() {
  if (await exists(benchJSON)) {
    const run = JSON.parse(await readFile(benchJSON, "utf8"));
    const out = new Map();
    for (const [key, m] of Object.entries(run.metrics ?? {})) {
      out.set(key, { ns: Number(m.ns ?? 0), bytes: Number(m.bytes ?? 0), allocs: Number(m.allocs ?? 0) });
    }
    return { metrics: out, source: benchJSON };
  }
  const benchText = await readFile(benchFile, "utf8");
  return { metrics: parseBench(benchText), source: benchFile };
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

function row(label, sub, wagoKey, wazeroKey, winWord, kind = "ns", forcedDelta = "") {
  const w = mustMetric(wagoKey);
  const z = mustMetric(wazeroKey);
  const wv = kind === "bytes" ? w.bytes : w.ns;
  const zv = kind === "bytes" ? z.bytes : z.ns;
  const max = Math.max(wv, zv, 1);
  const wWidth = barWidth(wv, max);
  const zWidth = barWidth(zv, max);
  const wWins = wv <= zv;
  const same = Math.abs(wv - zv) / Math.max(wv, zv, 1) < 0.03;
  const deltaClass = same ? "tie" : wWins ? "win" : "behind";
  const delta = forcedDelta || (same ? "same speed" : `${ratio(Math.max(wv, zv) / Math.max(Math.min(wv, zv), 1))}\u00d7${wWins ? ` ${winWord}` : " slower"}`);
  return {
    label,
    sub,
    wago: kind === "bytes" ? fmtBytes(w.bytes) : fmtNs(w.ns),
    wazero: kind === "bytes" ? fmtBytes(z.bytes) : fmtNs(z.ns),
    wWidth,
    zWidth,
    delta,
    deltaClass,
  };
}

function mustMetric(key) {
  const m = metrics.get(key);
  if (!m) throw new Error(`benchmark result missing: ${key}`);
  return m;
}

function barWidth(value, max) {
  if (value <= 0) return 4;
  return Math.max(4, Math.round((value / max) * 100));
}

function ratio(v) {
  return v >= 10 ? v.toFixed(1) : v.toFixed(1);
}

function fmtNs(ns) {
  if (ns >= 1e6) return trim(ns / 1e6, ns >= 10e6 ? 1 : 2) + "ms";
  if (ns >= 1e3) return trim(ns / 1e3, ns >= 100e3 ? 0 : 1) + "\u00b5s";
  return trim(ns, ns < 10 ? 1 : 1) + "ns";
}

function fmtBytes(bytes) {
  if (bytes >= 1 << 20) return trim(bytes / (1 << 20), 1) + " MB";
  if (bytes >= 1 << 10) return trim(bytes / (1 << 10), bytes >= 100 << 10 ? 0 : 1) + " KB";
  return `${bytes} B`;
}

function trim(v, digits) {
  return v.toFixed(digits).replace(/\.0$/, "");
}

function renderSection(topRows, detailRows) {
  return `            <section id="performance" class="section">
                <div class="eyebrow eyebrow--center">Performance</div>
                <h2 class="section__title">
                    Cold start in
                    <span class="section__title-accent">microseconds</span>
                </h2>
                <p class="section__lead">
                    One-shot compilation to native using the novel Valent-Block
                    architecture, avoiding complex SSA, IR, and optimization
                    passes while maintaining extraordinarily competitive
                    performance.
                </p>
                <div class="vs">
                    <div class="vs__legend">
                        <span class="vs__key"
                            ><i class="vs__dot vs__dot--wago"></i>wago</span
                        >
                        <span class="vs__key"
                            ><i class="vs__dot vs__dot--wazero"></i>wazero</span
                        >
                        <span class="vs__legend-note">shorter is faster</span>
                    </div>
${topRows.map((r) => renderRow(r, 20)).join("\n")}
                    <details class="vs__more">
                        <summary class="vs__more-head">
                            <span class="vs__more-title">More benchmarks</span>
                            <span class="vs__more-sub"
                                >fib loop · tree · hash · transcode</span
                            >
                            <span
                                class="vs__more-chevron"
                                aria-hidden="true"
                            ></span>
                        </summary>
                        <div class="vs__more-body">
${detailRows.map((r) => renderRow(r, 28)).join("\n")}
                        </div>
                    </details>
                </div>
                <p class="vs__foot">
                    Measured on linux/amd64 with the single-pass backend; wago
                    vs wazero over the same corpus. Numbers shift as the engine
                    evolves — see the
                    <a
                        href="https://github.com/wago-org/wago/tree/main/bench"
                        target="_blank"
                        rel="noopener"
                        >benchmark corpus &amp; methodology</a
                    >.
                </p>
            </section>
`;
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
