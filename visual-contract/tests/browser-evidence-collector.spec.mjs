import { execFile } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { promisify } from "node:util";
import { expect, test } from "@playwright/test";

const execFileAsync = promisify(execFile);
const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const collectorPath = path.join(repoRoot, "visual-contract/tools/browser-evidence-collector.mjs");
const samplePath = path.join(repoRoot, "visual-contract/testdata/web/sample-page.html");

test("collector emits stable local evidence with nearest selected parents", async ({}, testInfo) => {
  const evidencePath = testInfo.outputPath("sample-page.web-evidence.json");
  const screenshotPath = testInfo.outputPath("sample-page.png");

  await execFileAsync(process.execPath, [
    collectorPath,
    "--url",
    pathToFileURL(samplePath).href,
    "--scene-id",
    "sample_page",
    "--screenshot",
    screenshotPath,
    "--out",
    evidencePath,
  ]);

  const payload = JSON.parse(await fs.readFile(evidencePath, "utf8"));
  const evidence = payload.evidence;
  const nodes = new Map(evidence.nodes.map((node) => [node.id, node]));

  expect(evidence.scene_id).toBe("sample_page");
  expect(evidence.viewport).toEqual({ width_px: 1440, height_px: 900 });
  expect(evidence.screenshot_ref).toBe("sample-page.png");
  expect((await fs.stat(screenshotPath)).size).toBeGreaterThan(0);

  expect([...nodes.keys()]).toEqual([
    "hero_section",
    "node-1",
    "hero_title",
    "primary_cta",
    "preview_card",
  ]);
  expect(nodes.get("hero_section")).toMatchObject({ role: "hero", tag: "main" });
  expect(nodes.get("node-1")).toMatchObject({ parent_id: "hero_section", tag: "section" });
  expect(nodes.get("hero_title")).toMatchObject({ parent_id: "node-1", tag: "h1" });
  expect(nodes.get("primary_cta")).toMatchObject({ parent_id: "node-1", tag: "button" });
  expect(nodes.get("preview_card")).toMatchObject({
    parent_id: "hero_section",
    role: "visual_preview",
    tag: "aside",
  });

  const titleRight = nodes.get("hero_title").bounds_px.x + nodes.get("hero_title").bounds_px.w;
  expect(nodes.get("preview_card").bounds_px.x).toBeGreaterThan(titleRight);
});
