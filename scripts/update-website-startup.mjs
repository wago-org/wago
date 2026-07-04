#!/usr/bin/env node
// Regenerate ../website's Startup-latency section from bench/out/startup.json
// (produced by `node bench/startup/run.mjs`, the cross-runtime sweep).
//
// Mirrors scripts/update-website-bench.mjs: the section markup here is the
// source of truth, the numbers come from the dataset. It rewrites everything
// between the "STARTUP LATENCY" and "PERFORMANCE" anchor comments in index.html,
// then (unless WAGO_SITE_NOBUILD is set) runs the website's stats sync + build.
//
// Env: WAGO_STARTUP_JSON (dataset path), WAGO_WEBSITE_DIR (website checkout),
// WAGO_SITE_NOBUILD (skip npm sync/build — used by the umbrella that builds once).

import { access, readFile, writeFile } from "node:fs/promises";
import { constants } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = resolve(__dirname, "..");
const dataPath = resolve(process.env.WAGO_STARTUP_JSON || join(root, "bench", "startup", "startup.json"));
const websiteDir = resolve(process.env.WAGO_WEBSITE_DIR || join(root, "..", "website"));
const indexPath = join(websiteDir, "index.html");

const data = JSON.parse(await readFile(dataPath, "utf8"));
if (!Array.isArray(data.workloads) || !data.workloads.length) {
  throw new Error(`${dataPath}: no workloads`);
}

const html = await readFile(indexPath, "utf8");
const startAnchor = "            <!-- ░░░ STARTUP LATENCY ░░░ -->";
const endAnchor = "            <!-- ░░░ PERFORMANCE ░░░ -->";
const from = html.indexOf(startAnchor);
const to = html.indexOf(endAnchor, from + startAnchor.length);
if (from < 0 || to < 0) throw new Error("could not find the website startup section to replace");

const section = renderSection(data);
const updated = `${html.slice(0, from)}${startAnchor}\n${section}${html.slice(to)}`;
await writeFile(indexPath, updated);
console.log(`wago: updated website startup numbers from ${dataPath} (${data.workloads.length} workloads)`);

if (!process.env.WAGO_SITE_NOBUILD && (await exists(join(websiteDir, "package.json")))) {
  run("npm", ["run", "sync"], websiteDir);
  run("npm", ["run", "build"], websiteDir);
}

// ---- rendering -----------------------------------------------------------

// Format a millisecond value the way the panel does: integers at ≥100ms, one
// decimal below (5.79 → "5.8", 280.9 → "281").
function fmtMs(ms) {
  if (ms >= 100) return `${Math.round(ms)} ms`;
  return `${(Math.round(ms * 10) / 10).toFixed(1)} ms`;
}

// Bar scale excludes the LLVM outlier (wavm's ~audio-rate compile would flatten
// every other bar); wavm then caps at 100. Matches the hand-tuned panels.
function widths(rows, tagOf) {
  const scaleMax = Math.max(...rows.filter((r) => tagOf(r.name) !== "LLVM").map((r) => r.ms), 1);
  return rows.map((r) => Math.max(1, Math.min(100, Math.round((r.ms / scaleMax) * 100))));
}

function renderPanel(w, index, runtimes) {
  const tagOf = (n) => runtimes[n]?.tag ?? "";
  const rows = Object.entries(w.results)
    .map(([name, ms]) => ({ name, ms }))
    .sort((a, b) => a.ms - b.ms);
  const wds = widths(rows, tagOf);
  const body = rows
    .map((r, i) => {
      const isWago = r.name === "wago";
      const fill = isWago ? "vs__fill--wago" : "vs__fill--wazero";
      const rowClass = isWago ? "rank__row rank__row--wago" : "rank__row";
      return `                                <div class="${rowClass}">
                                    <span class="rank__name">${esc(r.name)}<span class="rank__tag">${esc(tagOf(r.name))}</span></span>
                                    <span class="vs__track"><span class="vs__fill ${fill}" data-bar data-width="${wds[i]}"></span></span>
                                    <span class="rank__val">${fmtMs(r.ms)}</span>
                                </div>`;
    })
    .join("\n");
  return `                            <div class="chart__panel rank" role="tabpanel" id="su-panel-${w.id}" aria-labelledby="su-tab-${w.id}"${index === 0 ? "" : " hidden"}>
${body}
                            </div>`;
}

function renderSection(d) {
  const tabs = d.workloads
    .map(
      (w, i) =>
        `                            <button class="chart__tab" role="tab" id="su-tab-${w.id}" aria-controls="su-panel-${w.id}" aria-selected="${i === 0 ? "true" : "false"}" tabindex="${i === 0 ? "0" : "-1"}">${esc(w.label)}</button>`
    )
    .join("\n");
  const panels = d.workloads.map((w, i) => renderPanel(w, i, d.runtimes ?? {})).join("\n");
  const count = numberWord(d.workloads.length);
  return `            <section id="startup" class="section">
                <div class="split split--startup">
                    <div>
                        <div class="split__eyebrow">Startup latency</div>
                        <h2 class="split__title">
                            Cold start in
                            <span class="section__title-accent">microseconds</span>
                        </h2>
                        <p class="split__body">
                            The whole process, from spawn to exit, timed
                            end-to-end across ${count} real workloads. wago lands
                            top-two on every one: it starts in milliseconds
                            <em>and</em> runs native.
                        </p>
                    </div>
                    <div class="chartcard">
                        <div class="chart__tabs" role="tablist" aria-label="Workload" data-tabs>
${tabs}
                        </div>
${panels}
                    </div>
                </div>
            </section>

`;
}

function numberWord(n) {
  return (
    ["zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve"][n] ??
    String(n)
  );
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
