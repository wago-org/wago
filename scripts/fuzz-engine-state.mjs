#!/usr/bin/env node

import { createHash, randomBytes } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import readline from "node:readline";
import { spawn } from "node:child_process";
import { pathToFileURL } from "node:url";
import { performance } from "node:perf_hooks";

import { intendedTrapClasses, observeInNode } from "./engine-state-oracle.mjs";

const DEFAULT_COUNT = 20;
const DEFAULT_SEED = "0x5eed";
const DEFAULT_TIMEOUT_MS = 10_000;
const textDecoder = new TextDecoder();

function usage() {
  return `Usage: scripts/fuzz-engine-state.sh [options]

Options:
  --count N          Number of generated cases (default: 20)
  --start N          First one-based case index (default: 1)
  --seed N|random    Unsigned 64-bit root seed (default: 0x5eed)
  --starshine PATH   Starshine WasmGC FFI binary
  --timeout-ms N     Railshot timeout for each case (default: 10000)
  --keep             Keep passing Wasm modules
  --help             Show this help`;
}

function optionValue(argv, index, name) {
  const argument = argv[index];
  if (argument.startsWith(`${name}=`)) return [argument.slice(name.length + 1), index];
  if (argument === name && index + 1 < argv.length) return [argv[index + 1], index + 1];
  throw new Error(`${name} requires a value`);
}

function parsePositiveInteger(value, name) {
  if (!/^\d+$/.test(value)) throw new Error(`${name} must be a positive integer`);
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed < 1) throw new Error(`${name} must be a positive integer`);
  return parsed;
}

function parseArguments(argv) {
  const options = {
    count: DEFAULT_COUNT,
    start: 1,
    seed: process.env.ENGINE_FUZZ_SEED || DEFAULT_SEED,
    starshine: process.env.STARSHINE_FFI_WASM || "../starshine-mb/dist/ffi/starshine-ffi.wasm",
    timeoutMs: DEFAULT_TIMEOUT_MS,
    worker: "",
    keep: false,
    help: false,
  };
  for (let index = 0; index < argv.length; index++) {
    const argument = argv[index];
    if (argument === "--keep") {
      options.keep = true;
    } else if (argument === "--help" || argument === "-h") {
      options.help = true;
    } else if (argument === "--count" || argument.startsWith("--count=")) {
      const [value, consumed] = optionValue(argv, index, "--count");
      options.count = parsePositiveInteger(value, "--count");
      index = consumed;
    } else if (argument === "--seed" || argument.startsWith("--seed=")) {
      [options.seed, index] = optionValue(argv, index, "--seed");
    } else if (argument === "--start" || argument.startsWith("--start=")) {
      const [value, consumed] = optionValue(argv, index, "--start");
      options.start = parsePositiveInteger(value, "--start");
      index = consumed;
    } else if (argument === "--starshine" || argument.startsWith("--starshine=")) {
      [options.starshine, index] = optionValue(argv, index, "--starshine");
    } else if (argument === "--worker" || argument.startsWith("--worker=")) {
      [options.worker, index] = optionValue(argv, index, "--worker");
    } else if (argument === "--timeout-ms" || argument.startsWith("--timeout-ms=")) {
      const [value, consumed] = optionValue(argv, index, "--timeout-ms");
      options.timeoutMs = parsePositiveInteger(value, "--timeout-ms");
      index = consumed;
    } else {
      throw new Error(`unknown option: ${argument}`);
    }
  }
  if (options.help) return options;
  if (!options.worker) throw new Error("--worker is required; use scripts/fuzz-engine-state.sh");
  if (options.start + options.count - 1 > 0x7fffffff) {
    throw new Error("--start plus --count must fit a positive i32 case index");
  }
  if (options.seed === "random") {
    options.rootSeed = randomBytes(8).readBigUInt64LE();
  } else {
    try {
      options.rootSeed = BigInt.asUintN(64, BigInt(options.seed));
    } catch {
      throw new Error(`invalid --seed value: ${options.seed}`);
    }
  }
  return options;
}

function makeImportStubs(module) {
  const imports = {};
  for (const descriptor of WebAssembly.Module.imports(module)) {
    if (descriptor.kind !== "function") {
      throw new Error(`unsupported Starshine FFI import kind: ${descriptor.kind}`);
    }
    imports[descriptor.module] ??= {};
    imports[descriptor.module][descriptor.name] = () => 0;
  }
  return imports;
}

async function loadGenerator(filename) {
  const bytes = await fs.readFile(filename);
  const module = new WebAssembly.Module(bytes);
  const exports = new WebAssembly.Instance(module, makeImportStubs(module)).exports;
  const get = (name) => {
    const value = exports[name];
    if (typeof value !== "function") throw new Error(`Starshine FFI export is missing: ${name}`);
    return value;
  };
  const byteField = (object, field) => {
    const length = get(`EncodedEngineStateCase::${field}_byte_length`)(object);
    const at = get(`EncodedEngineStateCase::${field}_byte_at`);
    return Uint8Array.from({ length }, (_, index) => at(object, index));
  };
  const textField = (object, field) => textDecoder.decode(byteField(object, field));
  return {
    ffiHash: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    generate(rootSeed, caseIndex) {
      const object = get("ffi_bridge::generate_engine_state_case")(BigInt.asIntN(64, rootSeed), caseIndex);
      if (!get("EncodedEngineStateCase::is_ok")(object)) {
        throw new Error(textField(object, "error"));
      }
      return {
        moduleBytes: byteField(object, "module"),
        caseIndex: get("EncodedEngineStateCase::case_index")(object),
        caseSeed: BigInt.asUintN(64, get("EncodedEngineStateCase::case_seed")(object)),
        profile: textField(object, "selected_profile"),
        expectsTrap: Boolean(get("EncodedEngineStateCase::expects_trap")(object)),
        trapFamily: textField(object, "trap_family"),
        attempts: get("EncodedEngineStateCase::generator_attempts")(object),
        staticInstructions: get("EncodedEngineStateCase::static_instruction_count")(object),
      };
    },
  };
}

class WorkerClient {
  constructor(command, timeoutMs) {
    this.timeoutMs = timeoutMs;
    this.pending = new Map();
    this.stderr = "";
    this.process = spawn(command, [], { stdio: ["pipe", "pipe", "pipe"] });
    this.process.stderr.setEncoding("utf8");
    this.process.stderr.on("data", (chunk) => {
      this.stderr = (this.stderr + chunk).slice(-65536);
    });
    const lines = readline.createInterface({ input: this.process.stdout });
    lines.on("line", (line) => this.receive(line));
    this.exited = new Promise((resolve) => {
      let settled = false;
      const finish = (result) => {
        if (settled) return;
        settled = true;
        resolve(result);
      };
      this.process.once("exit", (code, signal) => {
        const detail = signal ? `signal ${signal}` : `status ${code}`;
        const error = new Error(`Railshot worker exited with ${detail}${this.stderr ? `: ${this.stderr.trim()}` : ""}`);
        for (const item of this.pending.values()) {
          clearTimeout(item.timer);
          item.reject(error);
        }
        this.pending.clear();
        finish({ code, signal });
      });
      this.process.once("error", (error) => {
        for (const item of this.pending.values()) {
          clearTimeout(item.timer);
          item.reject(error);
        }
        this.pending.clear();
        finish({ code: null, signal: null, error });
      });
    });
  }

  receive(line) {
    let response;
    try {
      response = JSON.parse(line);
    } catch (error) {
      for (const item of this.pending.values()) {
        clearTimeout(item.timer);
        item.reject(new Error(`invalid Railshot response: ${error.message}`));
      }
      this.pending.clear();
      return;
    }
    const item = this.pending.get(response.id);
    if (!item) return;
    clearTimeout(item.timer);
    this.pending.delete(response.id);
    item.resolve(response);
  }

  request(request) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(request.id);
        reject(new Error(`Railshot case ${request.id} exceeded ${this.timeoutMs} ms`));
      }, this.timeoutMs);
      this.pending.set(request.id, { resolve, reject, timer });
      this.process.stdin.write(`${JSON.stringify(request)}\n`, (error) => {
        if (error) {
          clearTimeout(timer);
          this.pending.delete(request.id);
          reject(error);
        }
      });
    });
  }

  async close() {
    if (!this.process.stdin.destroyed) this.process.stdin.end();
    const result = await this.exited;
    if (result.error) throw result.error;
    if (result.code !== 0) throw new Error(`Railshot worker failed${this.stderr ? `: ${this.stderr.trim()}` : ""}`);
  }
}

function seedHex(seed) {
  return `0x${BigInt.asUintN(64, seed).toString(16).padStart(16, "0")}`;
}

function expectedOutcomeError(generated, observation) {
  const outcome = observation.events.find((event) => event[0] === "outcome");
  if (!outcome) return "Node oracle did not record an outcome";
  if (!generated.expectsTrap) {
    return outcome[1] === "returned" ? "" : `Starshine expected return; Node recorded ${outcome.slice(1).join(":")}`;
  }
  const expectedClass = intendedTrapClasses[generated.trapFamily];
  if (!expectedClass) return `Starshine supplied unknown trap family ${generated.trapFamily}`;
  if (outcome[1] !== "trapped" || outcome[2] !== expectedClass) {
    return `Starshine expected trap ${expectedClass}; Node recorded ${outcome.slice(1).join(":")}`;
  }
  return "";
}

async function writeFailure(modulePath, details) {
  const failurePath = `${modulePath}.failure.json`;
  await fs.writeFile(failurePath, `${JSON.stringify(details, null, 2)}\n`);
  return failurePath;
}

async function run(options) {
  const root = process.cwd();
  const starshinePath = path.resolve(root, options.starshine);
  const workerPath = path.resolve(root, options.worker);
  const temporaryRoot = path.join(root, ".tmp", "engine-state");
  await fs.mkdir(temporaryRoot, { recursive: true });
  const runDirectory = await fs.mkdtemp(path.join(temporaryRoot, `run-${seedHex(options.rootSeed).slice(2)}-`));
  const generator = await loadGenerator(starshinePath);
  const worker = new WorkerClient(workerPath, options.timeoutMs);
  const started = performance.now();
  const runDigest = createHash("sha256");
  let failed = false;
  try {
    const end = options.start + options.count;
    for (let caseIndex = options.start; caseIndex < end; caseIndex++) {
      const generated = generator.generate(options.rootSeed, caseIndex);
      if (generated.caseIndex !== caseIndex) throw new Error(`Starshine returned case index ${generated.caseIndex}; expected ${caseIndex}`);
      const moduleHash = createHash("sha256").update(generated.moduleBytes).digest("hex");
      const modulePath = path.join(runDirectory, `${String(caseIndex).padStart(8, "0")}-${moduleHash.slice(0, 16)}.wasm`);
      await fs.writeFile(modulePath, generated.moduleBytes);

      let node;
      let nodeError = "";
      try {
        node = observeInNode(generated.moduleBytes, generated.caseSeed);
      } catch (error) {
        nodeError = error.message;
      }
      let railshot;
      try {
        railshot = await worker.request({
          id: caseIndex,
          path: modulePath,
          case_seed: seedHex(generated.caseSeed),
        });
      } catch (error) {
        railshot = { id: caseIndex, status: "error", error: error.message };
      }
      const contractError = node ? expectedOutcomeError(generated, node) : `Node oracle failed: ${nodeError}`;
      if (contractError || railshot.status !== "complete" || railshot.hash !== node.hash) {
        failed = true;
        let diagnosticRailshot = railshot;
        if (railshot.status === "complete") {
          diagnosticRailshot = await worker.request({
            id: caseIndex,
            path: modulePath,
            case_seed: seedHex(generated.caseSeed),
            include_events: true,
          });
        }
        const failurePath = await writeFailure(modulePath, {
          schema: "starshine.engine-state-differential-failure.v1",
          root_seed: seedHex(options.rootSeed),
          case_seed: seedHex(generated.caseSeed),
          case_index: caseIndex,
          profile: generated.profile,
          intended_trap_family: generated.trapFamily || null,
          generator_attempts: generated.attempts,
          static_instruction_count: generated.staticInstructions,
          starshine_ffi_hash: generator.ffiHash,
          module_hash: `sha256:${moduleHash}`,
          contract_error: contractError || null,
          node: node ? { hash: node.hash, events: node.events } : { error: nodeError },
          railshot: diagnosticRailshot,
        });
        console.error(`FAIL case=${caseIndex} seed=${seedHex(generated.caseSeed)} artifact=${failurePath}`);
        break;
      }
      if (!options.keep) await fs.unlink(modulePath);
      runDigest.update(`${caseIndex}:${node.hash}\n`);
    }
  } finally {
    await worker.close();
  }

  const elapsedSeconds = (performance.now() - started) / 1000;
  if (failed) {
    process.exitCode = 1;
    return;
  }
  if (!options.keep) await fs.rm(runDirectory, { recursive: true });
  const rate = options.count / Math.max(elapsedSeconds, 0.001);
  const resultHash = `sha256:${runDigest.digest("hex")}`;
  const kept = options.keep ? ` artifacts=${runDirectory}` : "";
  console.log(`PASS cases=${options.count} start=${options.start} seed=${seedHex(options.rootSeed)} result=${resultHash} ffi=${generator.ffiHash} elapsed=${elapsedSeconds.toFixed(2)}s rate=${rate.toFixed(1)}/s${kept}`);
}

async function main() {
  try {
    const options = parseArguments(process.argv.slice(2));
    if (options.help) {
      console.log(usage());
      return;
    }
    await run(options);
  } catch (error) {
    console.error(`engine-state fuzz: ${error.message}`);
    process.exitCode = 1;
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  await main();
}
