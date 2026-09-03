#!/usr/bin/env node

import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const here = dirname(fileURLToPath(import.meta.url));
const benchDir = resolve(here, "..");
const options = parseArgs(process.argv.slice(2));
const manifest = JSON.parse(await readFile(options.manifest, "utf8"));
const work = await mkdtemp(join(tmpdir(), "wago-general-corpus-"));
const compileReports = [];
const runtimeRows = [];

try {
  for (const [moduleIndex, module] of manifest.modules.entries()) {
    console.error(`compile ${moduleIndex + 1}/${manifest.modules.length}: ${module.file}`);
    const wasm = join(benchDir, "corpus", module.file);
    const reportPath = join(work, `${basename(module.file, ".wasm")}-compile.json`);
    run(options.compilerHarness, [
      "-config", options.config,
      "-rounds", String(options.compileRounds),
      "-out", reportPath,
      wasm,
    ]);
    compileReports.push(JSON.parse(await readFile(reportPath, "utf8")));
  }

  for (const worker of options.runtimeWorkers) {
    const runtimePath = join(work, `${worker.engine}-runtime.jsonl`);
    for (let round = 0; round < options.runtimeRounds; round++) {
      console.error(`${worker.label} runtime round ${round + 1}/${options.runtimeRounds}`);
      for (const module of manifest.modules) {
        if (!Array.isArray(module.exec) || module.exec.length === 0) continue;
        for (let i = 0; i < module.exec.length; i++) {
          const entry = module.exec[i];
          const args = [
            ...worker.prefix,
            "-module", join(benchDir, "corpus", module.file),
            "-export", entry.export,
            "-args", (entry.args ?? []).join(","),
            "-round", String(round),
            "-benchtime-ns", String(options.benchtimeNs),
            "-out", runtimePath,
          ];
          if (module.init) args.push("-init", module.init);
          if (i === 0) args.push("-measure-instantiate");
          run(worker.command, args);
        }
      }
    }
    const runtimeText = await readFile(runtimePath, "utf8");
    for (const line of runtimeText.split("\n")) {
      if (line.trim()) runtimeRows.push(JSON.parse(line));
    }
  }
  await writeFile(options.out, `${JSON.stringify({
    version: 1,
    commit: options.commit,
    goos: compileReports[0]?.goos ?? "",
    goarch: compileReports[0]?.goarch ?? "",
    cpu: options.cpu,
    compileRounds: options.compileRounds,
    runtimeRounds: options.runtimeRounds,
    benchtimeNs: options.benchtimeNs,
    compile: compileReports,
    runtime: runtimeRows,
  }, null, 2)}\n`);
  console.log(`wrote ${options.out}: ${compileReports.length} modules, ${runtimeRows.length} external runtime samples`);
} finally {
  await rm(work, { recursive: true, force: true });
}

function parseArgs(args) {
  const values = new Map();
  for (let i = 0; i < args.length; i += 2) {
    if (!args[i]?.startsWith("--") || args[i + 1] === undefined) usage();
    values.set(args[i].slice(2), args[i + 1]);
  }
  const required = (name) => {
    const value = values.get(name);
    if (!value) usage();
    return resolve(value);
  };
  return {
    compilerHarness: required("compiler-harness"),
    runtimeWorkers: [
      { engine: "wasmtime", label: "Wasmtime", command: required("wasmtime-worker"), prefix: [] },
      { engine: "v8", label: "V8", command: values.get("v8") ?? "v8", prefix: [resolve(values.get("v8-worker") ?? join(here, "v8-execution-worker.js")), "--"] },
      { engine: "wavm", label: "WAVM", command: required("wavm-worker"), prefix: [] },
    ],
    config: resolve(values.get("config") ?? join(benchDir, "dragline-railshot-cranelift.json")),
    manifest: resolve(values.get("manifest") ?? join(benchDir, "corpus", "manifest.json")),
    out: required("out"),
    commit: values.get("commit") ?? "",
    cpu: values.get("cpu") ?? "",
    compileRounds: positiveInt(values.get("compile-rounds") ?? "6"),
    runtimeRounds: positiveInt(values.get("runtime-rounds") ?? "4"),
    benchtimeNs: positiveInt(values.get("benchtime-ns") ?? "100000000"),
  };
}

function positiveInt(value) {
  const number = Number(value);
  if (!Number.isSafeInteger(number) || number <= 0) usage();
  return number;
}

function usage() {
  console.error("usage: run-general-corpus.mjs --compiler-harness BIN --wasmtime-worker BIN --wavm-worker BIN --out FILE [--v8 BIN] [--v8-worker FILE] [--commit SHA] [--cpu NAME] [--config FILE] [--manifest FILE] [--compile-rounds N] [--runtime-rounds N] [--benchtime-ns N]");
  process.exit(2);
}

function run(command, args) {
  const result = spawnSync(command, args, { cwd: resolve(benchDir, ".."), encoding: "utf8" });
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} failed:\n${result.stderr || result.stdout}`);
  }
}
