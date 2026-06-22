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
const tallPath = path.join(repoRoot, "visual-contract/testdata/web/tall-page.html");

async function runCollector(testInfo, name, url, extraArgs = []) {
  const evidencePath = testInfo.outputPath(`${name}.web-evidence.json`);
  const screenshotPath = testInfo.outputPath(`${name}.png`);

  await execFileAsync(process.execPath, [
    collectorPath,
    "--url",
    pathToFileURL(url).href,
    "--scene-id",
    name,
    "--screenshot",
    screenshotPath,
    "--out",
    evidencePath,
    ...extraArgs,
  ]);

  return {
    screenshotPath,
    payload: JSON.parse(await fs.readFile(evidencePath, "utf8")),
  };
}

test("collector emits stable local evidence with nearest selected parents", async ({}, testInfo) => {
  const { payload, screenshotPath } = await runCollector(testInfo, "sample_page", samplePath);
  const evidence = payload.evidence;
  const nodes = new Map(evidence.nodes.map((node) => [node.id, node]));

  expect(evidence.scene_id).toBe("sample_page");
  expect(evidence.coordinate_space).toBe("viewport");
  expect(evidence.capture_mode).toBe("viewport-clipped");
  expect(evidence.viewport).toEqual({ width_px: 1920, height_px: 1080 });
  expect(evidence.screenshot_ref).toBe("sample_page.png");
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

test("collector clips viewport evidence and excludes fully offscreen nodes", async ({}, testInfo) => {
  const { payload } = await runCollector(testInfo, "tall_page", tallPath, [
    "--root-selector",
    "[data-visual-id='asha_shell']",
  ]);
  const evidence = payload.evidence;
  const nodes = new Map(evidence.nodes.map((node) => [node.id, node]));

  expect(evidence.viewport).toEqual({ width_px: 1920, height_px: 1080 });
  expect(evidence.coordinate_space).toBe("viewport");
  expect(evidence.capture_mode).toBe("viewport-clipped");
  expect(nodes.has("offscreen_log")).toBe(false);
  expect(nodes.get("asha_shell")).toMatchObject({ role: "app_shell" });
  expect(nodes.get("central_viewport")).toMatchObject({
    parent_id: "asha_shell",
    role: "central_3d_viewport",
  });
  expect(nodes.get("evidence_dock")).toMatchObject({
    parent_id: "asha_shell",
    bounds_clipped: true,
    original_bounds_px: { x: 1456, y: 1000, w: 400, h: 260 },
  });
  expect(nodes.get("evidence_dock").bounds_px).toEqual({ x: 1456, y: 1000, w: 400, h: 80 });
});

test("collector viewport mode includes intersecting nodes without clipping", async ({}, testInfo) => {
  const { payload } = await runCollector(testInfo, "tall_page_viewport", tallPath, [
    "--capture-mode",
    "viewport",
    "--root-selector",
    "[data-visual-id='asha_shell']",
  ]);
  const nodes = new Map(payload.evidence.nodes.map((node) => [node.id, node]));

  expect(nodes.has("offscreen_log")).toBe(false);
  expect(nodes.get("evidence_dock").bounds_px).toEqual({ x: 1456, y: 1000, w: 400, h: 260 });
  expect(nodes.get("evidence_dock").bounds_clipped).toBeUndefined();
});

test("collector page mode declares page coordinate space", async ({}, testInfo) => {
  const { payload } = await runCollector(testInfo, "tall_page_page", tallPath, [
    "--capture-mode",
    "page",
    "--root-selector",
    "[data-visual-id='asha_shell']",
  ]);
  const nodes = new Map(payload.evidence.nodes.map((node) => [node.id, node]));

  expect(payload.evidence.coordinate_space).toBe("page");
  expect(payload.evidence.capture_mode).toBe("page");
  expect(payload.evidence.page_size.height_px).toBeGreaterThan(payload.evidence.viewport.height_px);
  expect(nodes.has("offscreen_log")).toBe(true);
  expect(nodes.get("offscreen_log").bounds_px.y).toBeGreaterThan(payload.evidence.viewport.height_px);
});
