#!/usr/bin/env node
// Regenerate ../website's end-to-end latency section from matched ARM64 and
// AMD64 captures produced by bench/startup/run.mjs.

import { access, readFile, writeFile } from "node:fs/promises";
import { constants } from "node:fs";
import { spawnSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, "..");
const REQUIRED_RUNTIMES = ["wago", "wazero", "wasmtime", "v8", "wasm3", "wasmi", "wavm"];
const REQUIRED_TAGS = {
  wago: "single-pass",
  wazero: "compiler",
  wasmtime: "cranelift",
  v8: "turboshaft",
  wasm3: "interpreter",
  wasmi: "interpreter",
  wavm: "llvm",
};
const dataPaths = {
  arm64: resolve(process.env.WAGO_STARTUP_JSON_ARM64 || join(ROOT, "bench", "startup", "startup-arm64.json")),
  amd64: resolve(process.env.WAGO_STARTUP_JSON_AMD64 || join(ROOT, "bench", "startup", "startup-amd64.json")),
};
const websiteDir = resolve(process.env.WAGO_WEBSITE_DIR || join(ROOT, "..", "website"));
const indexPath = join(websiteDir, "index.html");

const datasets = {};
for (const [architecture, path] of Object.entries(dataPaths)) {
  const data = JSON.parse(await readFile(path, "utf8"));
  validateDataset(data, architecture, path);
  datasets[architecture] = data;
}
validatePair(datasets.arm64, datasets.amd64);

const html = await readFile(indexPath, "utf8");
const startAnchor = "            <!-- ░░░ END-TO-END LATENCY ░░░ -->";
const endAnchor = "            <!-- ░░░ PERFORMANCE ░░░ -->";
const from = html.indexOf(startAnchor);
const to = html.indexOf(endAnchor, from + startAnchor.length);
if (from < 0 || to < 0) throw new Error("could not find the website end-to-end section to replace");

const updated = `${html.slice(0, from)}${startAnchor}\n${renderSection(datasets)}${html.slice(to)}`;
await writeFile(indexPath, updated);
console.log(`wago: updated website end-to-end numbers for ARM64 and AMD64 (${datasets.arm64.workloads.length} workloads)`);

if (!process.env.WAGO_SITE_NOBUILD && (await exists(join(websiteDir, "package.json")))) {
  run("npm", ["run", "sync"], websiteDir);
  run("npm", ["run", "build"], websiteDir);
}

function validateDataset(data, architecture, path) {
  if (data.architecture !== architecture) throw new Error(`${path}: architecture ${data.architecture ?? "missing"}, want ${architecture}`);
  if (data.unit !== "ms") throw new Error(`${path}: unit ${data.unit ?? "missing"}, want ms`);
  if (!data.sourceCommit) throw new Error(`${path}: sourceCommit is required`);
  if (!Array.isArray(data.workloads) || data.workloads.length === 0) throw new Error(`${path}: no workloads`);
  const ids = data.workloads.map((workload) => workload.id);
  if (new Set(ids).size !== ids.length) throw new Error(`${path}: duplicate workload ids`);
  assertKeys(data.runtimes ?? {}, REQUIRED_RUNTIMES, `${path}: runtime metadata`);
  for (const name of REQUIRED_RUNTIMES) {
    if (data.runtimes[name]?.tag !== REQUIRED_TAGS[name]) {
      throw new Error(`${path}: ${name} tag ${data.runtimes[name]?.tag ?? "missing"}, want ${REQUIRED_TAGS[name]}`);
    }
  }
  for (const workload of data.workloads) {
    assertKeys(workload.results ?? {}, REQUIRED_RUNTIMES, `${path}: ${workload.id} results`);
    for (const [name, value] of Object.entries(workload.results)) {
      if (!Number.isFinite(value) || value <= 0) throw new Error(`${path}: ${workload.id}/${name} must be a positive number`);
    }
  }
}

function validatePair(arm64, amd64) {
  if (arm64.sourceCommit !== amd64.sourceCommit) {
    throw new Error(`sourceCommit mismatch: ARM64 ${arm64.sourceCommit}, AMD64 ${amd64.sourceCommit}`);
  }
  const armIds = arm64.workloads.map((workload) => workload.id);
  const amdIds = amd64.workloads.map((workload) => workload.id);
  if (JSON.stringify(armIds) !== JSON.stringify(amdIds)) throw new Error("ARM64 and AMD64 workload sets differ");
  for (const name of REQUIRED_RUNTIMES) {
    const armMeta = arm64.runtimes[name];
    const amdMeta = amd64.runtimes[name];
    if (armMeta.label !== amdMeta.label || armMeta.tag !== amdMeta.tag) {
      throw new Error(`ARM64 and AMD64 runtime metadata differ for ${name}`);
    }
  }
}

function assertKeys(object, wanted, context) {
  const got = Object.keys(object).sort();
  const expected = [...wanted].sort();
  if (JSON.stringify(got) !== JSON.stringify(expected)) {
    throw new Error(`${context}: got [${got.join(", ")}], want [${expected.join(", ")}]`);
  }
}

function fmtMs(ms) {
  if (ms >= 100) return `${Math.round(ms)} ms`;
  return `${(Math.round(ms * 10) / 10).toFixed(1)} ms`;
}

function widths(rows) {
  const maximum = Math.max(...rows.map((row) => row.ms), 1);
  return rows.map((row) => row.ms === maximum
    ? 100
    : Math.max(1, Math.min(99, Math.round((row.ms / maximum) * 100))));
}

function renderPanel(workload, index, runtimes, architecture) {
  const rows = Object.entries(workload.results)
    .map(([name, ms]) => ({ name, ms }))
    .sort((a, b) => a.ms - b.ms);
  const rowWidths = widths(rows);
  const body = rows.map((row, rowIndex) => {
    const isWago = row.name === "wago";
    const label = isWago
      ? `wago<span class="rank__mode">single-pass</span>`
      : `${esc(runtimes[row.name].label ?? row.name)}<span class="rank__tag">${esc(runtimes[row.name].tag)}</span>`;
    return `                                <div class="rank__row${isWago ? " rank__row--wago" : ""}">
                                    <span class="rank__name">${label}</span>
                                    <span class="vs__track"><span class="vs__fill ${isWago ? "vs__fill--railshot" : "vs__fill--wazero"}" data-bar data-value="${row.ms}" data-width="${rowWidths[rowIndex]}"></span></span>
                                    <span class="rank__val">${fmtMs(row.ms)}</span>
                                </div>`;
  }).join("\n");
  return `                                <div class="chart__panel rank" role="tabpanel" id="su-${architecture}-panel-${workload.id}" aria-labelledby="su-${architecture}-tab-${workload.id}" data-relative-bars${index === 0 ? "" : " hidden"}>
${body}
                                </div>`;
}

function renderArchitecture(data, architecture, hidden) {
  const tabs = data.workloads.map((workload, index) =>
    `                                <button class="chart__tab" role="tab" id="su-${architecture}-tab-${workload.id}" aria-controls="su-${architecture}-panel-${workload.id}" aria-selected="${index === 0 ? "true" : "false"}" tabindex="${index === 0 ? "0" : "-1"}">${esc(workload.label)}</button>`
  ).join("\n");
  const panels = data.workloads.map((workload, index) => renderPanel(workload, index, data.runtimes, architecture)).join("\n");
  return `                            <div id="startup-arch-panel-${architecture}" data-startup-arch-panel="${architecture}"${hidden ? " hidden" : ""}>
                                <div class="chart__tabs" role="tablist" aria-label="Workload" data-tabs>
${tabs}
                                </div>
${panels}
                                <div class="chart__machine">${esc(data.machine)}</div>
                            </div>`;
}

function renderSection(all) {
  return `            <section id="latency" class="section">
                <div class="split split--startup">
                    <div>
                        <div class="split__eyebrow">End-to-end latency</div>
                        <h2 class="split__title">
                            Fresh process in
                            <span class="section__title-accent">milliseconds</span>
                        </h2>
                        <p class="split__body">
                            The whole process, from spawn to exit, timed
                            end-to-end across ${numberWord(all.arm64.workloads.length)} real workloads. Compare
                            process startup and execution together on the
                            architecture that matches your machine.
                        </p>
                    </div>
                    <div class="chartcard">
                        <div class="chart__architectures" role="tablist" aria-label="End-to-end benchmark architecture" data-startup-arch-toggle>
                            <button class="chart__archtab" type="button" role="tab" aria-selected="true" aria-controls="startup-arch-panel-arm64" data-startup-arch-target="arm64">ARM64</button>
                            <button class="chart__archtab" type="button" role="tab" aria-selected="false" aria-controls="startup-arch-panel-amd64" data-startup-arch-target="amd64" tabindex="-1">AMD64</button>
                        </div>
${renderArchitecture(all.arm64, "arm64", false)}
${renderArchitecture(all.amd64, "amd64", true)}
                    </div>
                </div>
            </section>

`;
}

function numberWord(number) {
  return ["zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve"][number] ?? String(number);
}

function esc(value) {
  return String(value).replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

async function exists(path) {
  try { await access(path, constants.R_OK); return true; } catch { return false; }
}

function run(command, args, cwd) {
  const result = spawnSync(command, args, { cwd, stdio: "inherit" });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`${command} ${args.join(" ")} failed with exit ${result.status}`);
}
