#!/usr/bin/env node
// Cross-runtime startup-latency sweep → bench/out/startup.json.
//
// Times the whole process (exec → load → compile → instantiate → run _start →
// exit) for each committed work-twin in bench/startup/twins/ across every
// runtime in runtimes.json, using hyperfine (cold caches, same knobs the
// website panels were first measured with). See skills/startup-latency-bench
// for the methodology and how the twins are built.
//
// A runtime whose binary isn't on PATH (nor via its *_BIN env override) is
// skipped with a warning, so the sweep still produces a partial dataset on a
// machine that lacks some engines. The website generator
// (scripts/update-website-startup.mjs) consumes the JSON this writes.
//
// Usage:
//   node bench/startup/run.mjs                 # full sweep → bench/out/startup.json
//   node bench/startup/run.mjs --out x.json    # write elsewhere
//   WARMUP=5 MINRUNS=30 node bench/startup/run.mjs
//   WAGO_BIN=/path/to/wago WASM3_BIN=... node bench/startup/run.mjs

import { readFile, writeFile, mkdir, access, rm } from "node:fs/promises";
import { constants, accessSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO = resolve(HERE, "..", "..");
const cfg = JSON.parse(await readFile(join(HERE, "runtimes.json"), "utf8"));
const outPath = resolve(argOf("--out") || join(HERE, "startup.json"));
const WARMUP = process.env.WARMUP || "5";
const MINRUNS = process.env.MINRUNS || "30";

const HYPERFINE = which(process.env.HYPERFINE_BIN || "hyperfine");
if (!HYPERFINE) fail("hyperfine not found (set HYPERFINE_BIN or install it)");

// Resolve each runtime's binary once; drop the ones we can't find.
const resolved = {};
for (const name of cfg.order) {
  const rt = cfg.runtimes[name];
  const bin = which(process.env[rt.env] || rt.bin);
  if (bin) resolved[name] = { ...rt, bin };
  else console.warn(`! ${name}: binary not found (${rt.env}=… to point at it) — skipping`);
}
const active = cfg.order.filter((n) => resolved[n]);
if (!active.length) fail("no runtimes found");
console.log(`startup sweep: ${active.length}/${cfg.order.length} runtimes · ${cfg.workloads.length} workloads`);

const workloads = [];
for (const w of cfg.workloads) {
  const wasm = join(HERE, "twins", w.twin);
  if (!(await exists(wasm))) {
    console.warn(`! ${w.id}: twin ${w.twin} missing — skipping workload`);
    continue;
  }
  // One hyperfine invocation per workload, one named command per runtime.
  const jsonTmp = join(dirname(outPath), `.startup-${w.id}.json`);
  const args = ["-N", "--warmup", WARMUP, "--min-runs", MINRUNS, "--export-json", jsonTmp];
  for (const name of active) {
    const r = resolved[name];
    const cmd = [r.bin, ...r.args.map((a) => a.replaceAll("{wasm}", wasm))].join(" ");
    args.push("-n", name, cmd);
  }
  process.stdout.write(`  ${w.id} … `);
  await mkdir(dirname(outPath), { recursive: true });
  const res = spawnSync(HYPERFINE, args, { encoding: "utf8", maxBuffer: 64 << 20 });
  if (res.status !== 0) fail(`hyperfine failed for ${w.id}: ${res.stderr || res.stdout}`);
  const hf = JSON.parse(await readFile(jsonTmp, "utf8"));
  await rm(jsonTmp, { force: true });
  const results = {};
  for (const r of hf.results) results[r.command] = round(r.mean * 1000, 3); // s → ms
  console.log(active.map((n) => `${n} ${results[n]}`).join("  "));
  workloads.push({ id: w.id, label: w.label, desc: w.desc, results });
}

const data = {
  generated: new Date().toISOString().slice(0, 10),
  machine: process.env.STARTUP_MACHINE || cpuName(),
  method: `hyperfine -N --warmup ${WARMUP} --min-runs ${MINRUNS}; cold caches (full process, exec→exit)`,
  unit: "ms",
  runtimes: Object.fromEntries(cfg.order.map((n) => [n, { tag: cfg.runtimes[n].tag }])),
  workloads,
};

await mkdir(dirname(outPath), { recursive: true });
await writeFile(outPath, JSON.stringify(data, null, 2) + "\n");
console.log(`wrote ${outPath} (${workloads.length} workloads, ${active.length} runtimes)`);

// ---- helpers -------------------------------------------------------------
function argOf(flag) {
  const i = process.argv.indexOf(flag);
  return i >= 0 ? process.argv[i + 1] : "";
}
function which(bin) {
  if (bin.includes("/")) return existsSync(bin) ? bin : "";
  const r = spawnSync("command", ["-v", bin], { shell: true, encoding: "utf8" });
  return r.status === 0 ? r.stdout.trim() : "";
}
function existsSync(p) {
  try { accessSync(p, constants.X_OK); return true; } catch { return false; }
}
async function exists(p) {
  try { await access(p, constants.R_OK); return true; } catch { return false; }
}
function cpuName() {
  const r = spawnSync("sh", ["-c", "grep -m1 'model name' /proc/cpuinfo | cut -d: -f2"], { encoding: "utf8" });
  const cpu = (r.stdout || "").trim();
  return cpu ? `${cpu}, ${process.platform}/${process.arch}` : `${process.platform}/${process.arch}`;
}
function round(v, d) { const f = 10 ** d; return Math.round(v * f) / f; }
function fail(msg) { console.error("startup sweep:", msg); process.exit(1); }
