#!/usr/bin/env node
// Cross-runtime end-to-end latency sweep → one architecture-specific JSON file.
//
// Times the full process (spawn → load → compile → instantiate → run _start →
// exit) for every committed work twin and every configured runtime. A complete
// runtime set is required so the website never publishes unmatched rows.

import { access, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { accessSync, constants } from "node:fs";
import { spawnSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO = resolve(HERE, "..", "..");
const cfg = JSON.parse(await readFile(join(HERE, "runtimes.json"), "utf8"));
const architecture = process.arch === "x64" ? "amd64" : process.arch;
const outPath = resolve(argOf("--out") || join(HERE, `startup-${architecture}.json`));
const WARMUP = process.env.WARMUP || "5";
const MINRUNS = process.env.MINRUNS || "30";

const HYPERFINE = which(process.env.HYPERFINE_BIN || "hyperfine");
if (!HYPERFINE) fail("hyperfine not found (set HYPERFINE_BIN or install it)");

const resolved = {};
const missing = [];
for (const name of cfg.order) {
  const runtime = cfg.runtimes[name];
  const bin = which(process.env[runtime.env] || runtime.bin);
  if (bin) resolved[name] = { ...runtime, bin };
  else missing.push(`${name} (${runtime.env})`);
}
if (missing.length) fail(`missing required runtimes: ${missing.join(", ")}`);
console.log(`end-to-end sweep: ${cfg.order.length} runtimes · ${cfg.workloads.length} workloads · ${architecture}`);

const versions = Object.fromEntries(cfg.order.map((name) => [name, runtimeVersion(resolved[name])]));
const workloads = [];
for (const workload of cfg.workloads) {
  const wasm = join(HERE, "twins", workload.twin);
  if (!(await exists(wasm))) fail(`${workload.id}: twin ${workload.twin} is missing`);

  const commands = Object.fromEntries(cfg.order.map((name) => {
    const runtime = resolved[name];
    const args = runtime.args.map((arg) => substitute(arg, wasm));
    const check = spawnSync(runtime.bin, args, { encoding: "utf8", maxBuffer: 64 << 20 });
    if (check.status !== 0) {
      fail(`${name} correctness preflight failed for ${workload.id}: ${check.stderr || check.stdout || `exit ${check.status}`}`);
    }
    return [name, [runtime.bin, ...args].map(shellQuote).join(" ")];
  }));

  const jsonTmp = join(dirname(outPath), `.startup-${architecture}-${workload.id}.json`);
  const args = ["-N", "--warmup", WARMUP, "--min-runs", MINRUNS, "--export-json", jsonTmp];
  for (const name of cfg.order) args.push("-n", name, commands[name]);
  process.stdout.write(`  ${workload.id} … `);
  await mkdir(dirname(outPath), { recursive: true });
  const result = spawnSync(HYPERFINE, args, { encoding: "utf8", maxBuffer: 64 << 20 });
  if (result.status !== 0) fail(`hyperfine failed for ${workload.id}: ${result.stderr || result.stdout}`);
  const hyperfine = JSON.parse(await readFile(jsonTmp, "utf8"));
  await rm(jsonTmp, { force: true });
  const results = {};
  for (const row of hyperfine.results) results[row.command] = round(row.mean * 1000, 3);
  console.log(cfg.order.map((name) => `${name} ${results[name]} ms`).join("  "));
  workloads.push({ id: workload.id, label: workload.label, desc: workload.desc, results });
}

const data = {
  generated: new Date().toISOString().slice(0, 10),
  sourceCommit: gitCommit(),
  machine: process.env.STARTUP_MACHINE || cpuName(),
  method: `hyperfine -N --warmup ${WARMUP} --min-runs ${MINRUNS}; fresh process per run (spawn→exit)`,
  unit: "ms",
  architecture,
  runtimes: Object.fromEntries(cfg.order.map((name) => [name, {
    label: cfg.runtimes[name].label ?? name,
    tag: cfg.runtimes[name].tag,
    version: versions[name],
  }])),
  workloads,
};

await mkdir(dirname(outPath), { recursive: true });
await writeFile(outPath, JSON.stringify(data, null, 2) + "\n");
console.log(`wrote ${outPath} (${workloads.length} workloads, ${cfg.order.length} runtimes)`);

function argOf(flag) {
  const index = process.argv.indexOf(flag);
  return index >= 0 ? process.argv[index + 1] : "";
}

function substitute(arg, wasm) {
  return arg.replaceAll("{repo}", REPO).replaceAll("{wasm}", wasm);
}

function which(bin) {
  if (bin.includes("/")) return existsSync(bin) ? bin : "";
  const result = spawnSync("command", ["-v", bin], { shell: true, encoding: "utf8" });
  return result.status === 0 ? result.stdout.trim() : "";
}

function existsSync(path) {
  try { accessSync(path, constants.X_OK); return true; } catch { return false; }
}

async function exists(path) {
  try { await access(path, constants.R_OK); return true; } catch { return false; }
}

function runtimeVersion(runtime) {
  const result = spawnSync(runtime.bin, runtime.versionArgs ?? ["--version"], { encoding: "utf8" });
  const value = `${result.stdout || ""}\n${result.stderr || ""}`.trim().split("\n", 1)[0];
  return value || "unknown";
}

function cpuName() {
  const command = process.platform === "darwin"
    ? ["sysctl", ["-n", "machdep.cpu.brand_string"]]
    : ["sh", ["-c", "grep -m1 'model name' /proc/cpuinfo | cut -d: -f2"]];
  const result = spawnSync(command[0], command[1], { encoding: "utf8" });
  const cpu = (result.stdout || "").trim();
  return cpu ? `${cpu}, ${process.platform}/${architecture}` : `${process.platform}/${architecture}`;
}

function gitCommit() {
  const result = spawnSync("git", ["rev-parse", "HEAD"], { cwd: REPO, encoding: "utf8" });
  return result.status === 0 ? result.stdout.trim() : "";
}

function shellQuote(value) {
  return `'${String(value).replaceAll("'", `'\\''`)}'`;
}

function round(value, digits) {
  const factor = 10 ** digits;
  return Math.round(value * factor) / factor;
}

function fail(message) {
  console.error("end-to-end sweep:", message);
  process.exit(1);
}
