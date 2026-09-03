#!/usr/bin/env node
// Regenerate ../website's end-to-end latency section from matched ARM64 and
// AMD64 startup captures produced by `node bench/startup/run.mjs`.
//
// Mirrors scripts/update-website-bench.mjs: the section markup here is the
// source of truth, the numbers come from the dataset. It rewrites everything
// between the "END-TO-END LATENCY" and "PERFORMANCE" anchors in index.html,
// then (unless WAGO_SITE_NOBUILD is set) runs the website's stats sync + build.
//
// Env: WAGO_STARTUP_JSON_{ARM64,AMD64} (dataset paths), WAGO_WEBSITE_DIR
// (website checkout), WAGO_SITE_NOBUILD (skip npm sync/build).

import { access, readFile, writeFile } from "node:fs/promises";
import { constants } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = resolve(__dirname, "..");
const dataPaths = {
  arm64: resolve(process.env.WAGO_STARTUP_JSON_ARM64 || join(root, "bench", "startup", "startup-arm64.json")),
  amd64: resolve(process.env.WAGO_STARTUP_JSON_AMD64 || join(root, "bench", "startup", "startup-amd64.json")),
};
const websiteDir = resolve(process.env.WAGO_WEBSITE_DIR || join(root, "..", "website"));
const indexPath = join(websiteDir, "index.html");

const datasets = {};
for (const [arch, path] of Object.entries(dataPaths)) {
  const data = JSON.parse(await readFile(path, "utf8"));
  if (data.architecture !== arch) throw new Error(`${path}: architecture ${data.architecture ?? "missing"}, want ${arch}`);
  if (!Array.isArray(data.workloads) || !data.workloads.length) throw new Error(`${path}: no workloads`);
  datasets[arch] = data;
}

const html = await readFile(indexPath, "utf8");
const startAnchor = "            <!-- ░░░ END-TO-END LATENCY ░░░ -->";
const endAnchor = "            <!-- ░░░ PERFORMANCE ░░░ -->";
const from = html.indexOf(startAnchor);
const to = html.indexOf(endAnchor, from + startAnchor.length);
if (from < 0 || to < 0) throw new Error("could not find the website startup section to replace");

const section = renderSection(datasets);
const updated = `${html.slice(0, from)}${startAnchor}\n${section}${html.slice(to)}`;
await writeFile(indexPath, updated);
console.log(`wago: updated website end-to-end numbers for ARM64 and AMD64 (${datasets.arm64.workloads.length} workloads)`);

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

// The slowest runtime fills the rail; every other width is relative to its
// value. Cap rounded non-max values below 100 so only the true maximum fills it.
function widths(rows) {
  const scaleMax = Math.max(...rows.map((r) => r.ms), 1);
  return rows.map((r) => r.ms === scaleMax
    ? 100
    : Math.max(1, Math.min(99, Math.round((r.ms / scaleMax) * 100))));
}

function renderPanel(w, index, runtimes, arch) {
  const tagOf = (n) => runtimes[n]?.tag ?? "";
  const rows = Object.entries(w.results)
    .map(([name, ms]) => ({ name, ms }))
    .sort((a, b) => a.ms - b.ms);
  const wds = widths(rows);
  const body = rows
    .map((r, i) => {
      const isWago = r.name === "railshot" || r.name === "dragline";
      const fill = r.name === "dragline" ? "vs__fill--dragline" : isWago ? "vs__fill--railshot" : "vs__fill--wazero";
      const rowClass = isWago ? "rank__row rank__row--wago" : "rank__row";
      return `                                <div class="${rowClass}">
                                    <span class="rank__name">${esc(runtimes[r.name]?.label ?? r.name)}${isWago ? "" : `<span class="rank__tag">${esc(tagOf(r.name))}</span>`}</span>
                                    <span class="vs__track"><span class="vs__fill ${fill}" data-bar data-value="${r.ms}" data-width="${wds[i]}"></span></span>
                                    <span class="rank__val">${fmtMs(r.ms)}</span>
                                </div>`;
    })
    .join("\n");
  return `                                <div class="chart__panel rank" role="tabpanel" id="su-${arch}-panel-${w.id}" aria-labelledby="su-${arch}-tab-${w.id}" data-relative-bars${index === 0 ? "" : " hidden"}>
${body}
                                </div>`;
}

function renderArchitecture(d, arch, hidden) {
  const tabs = d.workloads
    .map(
      (w, i) =>
        `                                <button class="chart__tab" role="tab" id="su-${arch}-tab-${w.id}" aria-controls="su-${arch}-panel-${w.id}" aria-selected="${i === 0 ? "true" : "false"}" tabindex="${i === 0 ? "0" : "-1"}">${esc(w.label)}</button>`
    )
    .join("\n");
  const panels = d.workloads.map((w, i) => renderPanel(w, i, d.runtimes ?? {}, arch)).join("\n");
  return `                            <div id="startup-arch-panel-${arch}" data-startup-arch-panel="${arch}"${hidden ? " hidden" : ""}>
                                <div class="chart__tabs" role="tablist" aria-label="Workload" data-tabs>
${tabs}
                                </div>
${panels}
                                <div class="chart__machine">${esc(d.machine)}</div>
                            </div>`;
}

function renderSection(all) {
  const count = numberWord(all.arm64.workloads.length);
  return `            <section id="latency" class="section">
                <div class="split split--startup">
                    <div>
                        <div class="split__eyebrow">End-to-end latency</div>
                        <h2 class="split__title">
                            Cold start in
                            <span class="section__title-accent">milliseconds</span>
                        </h2>
                        <p class="split__body">
                            The whole process, from spawn to exit, timed
                            end-to-end across ${count} real workloads. Compare
                            cold process cost and real execution together on
                            the architecture that matches your machine.
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
