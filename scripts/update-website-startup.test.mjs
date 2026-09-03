import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import test from "node:test";

const root = resolve(import.meta.dirname, "..");

test("renders matched architecture panels with Dragline", () => {
  const website = mkdtempSync(join(tmpdir(), "wago-startup-site-"));
  writeFileSync(
    join(website, "index.html"),
    "            <!-- ░░░ END-TO-END LATENCY ░░░ -->\nold\n            <!-- ░░░ PERFORMANCE ░░░ -->\n",
  );

  execFileSync(process.execPath, [join(root, "scripts", "update-website-startup.mjs")], {
    env: {
      ...process.env,
      WAGO_SITE_NOBUILD: "1",
      WAGO_WEBSITE_DIR: website,
    },
  });

  const html = readFileSync(join(website, "index.html"), "utf8");
  assert.equal(matches(html, /data-startup-arch-target=/g), 2);
  assert.equal(matches(html, /data-startup-arch-panel=/g), 2);
  assert.match(html, /data-startup-arch-target="arm64"/);
  assert.match(html, /data-startup-arch-target="amd64"/);
  assert.match(html, />Dragline</);
  assert.match(html, />Railshot</);
  assert.match(html, /Apple M4 Max, darwin\/arm64/);
  assert.match(html, /AMD Ryzen 7 7800X3D, linux\/amd64, CPU 7 pinned/);
});

function matches(text, pattern) {
  return text.match(pattern)?.length ?? 0;
}
