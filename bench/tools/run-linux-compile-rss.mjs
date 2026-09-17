#!/usr/bin/env node

import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const here = dirname(fileURLToPath(import.meta.url));
const benchDir = resolve(here, "..");
const options = parseArgs(process.argv.slice(2));
const config = JSON.parse(await readFile(options.config, "utf8"));
const manifest = JSON.parse(await readFile(options.manifest, "utf8"));
const work = await mkdtemp(join(tmpdir(), "wago-linux-rss-"));
const samples = [];

try {
  for (const [moduleIndex, module] of manifest.modules.entries()) {
    console.error(`RSS ${moduleIndex + 1}/${manifest.modules.length}: ${module.file}`);
    const wasm = join(benchDir, "corpus", module.file);
    for (const engine of config.engines) {
      const artifact = join(work, `${basename(module.file, ".wasm")}-${engine.name}.bin`);
      const rss = join(work, "peak-rss-kib.txt");
      const [command, args] = compilerCommand(engine, wasm, artifact);
      const result = spawnSync(options.timeCommand, ["-f", "%M", "-o", rss, "--", command, ...args], {
        cwd: resolve(benchDir, ".."),
        encoding: "utf8",
      });
      if (result.status !== 0) {
        throw new Error(`${command} ${args.join(" ")} failed:\n${result.stderr || result.stdout}`);
      }
      const peakKiB = Number((await readFile(rss, "utf8")).trim());
      if (!Number.isSafeInteger(peakKiB) || peakKiB <= 0) {
        throw new Error(`invalid peak RSS for ${engine.name}/${module.file}: ${peakKiB}`);
      }
      samples.push({ module: module.file, engine: engine.name, peak_rss_bytes: peakKiB * 1024 });
    }
  }
  await writeFile(options.out, `${JSON.stringify({ version: 1, samples }, null, 2)}\n`);
  console.log(`wrote ${options.out}: ${samples.length} Linux peak-RSS samples`);
} finally {
  await rm(work, { recursive: true, force: true });
}

function compilerCommand(engine, wasm, artifact) {
  if (engine.builtin) {
    return [options.compilerHarness, [
      `-internal-worker=${engine.builtin}`,
      `-internal-target=${engine.target || "compat"}`,
      `-internal-workers=${engine.workers || 0}`,
      wasm,
      artifact,
    ]];
  }
  return [engine.command, engine.args.map((arg) => arg.replaceAll("{wasm}", wasm).replaceAll("{artifact}", artifact))];
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
    out: required("out"),
    config: resolve(values.get("config") ?? join(benchDir, "dragline-railshot-cranelift.json")),
    manifest: resolve(values.get("manifest") ?? join(benchDir, "corpus", "manifest.json")),
    timeCommand: values.get("time-command") ?? "/usr/bin/time",
  };
}

function usage() {
  console.error("usage: run-linux-compile-rss.mjs --compiler-harness BIN --out FILE [--config FILE] [--manifest FILE] [--time-command BIN]");
  process.exit(2);
}
