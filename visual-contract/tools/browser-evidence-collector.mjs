#!/usr/bin/env node
import fs from "node:fs/promises";
import path from "node:path";
import { chromium } from "@playwright/test";

function usage() {
  console.error("usage: browser-evidence-collector.mjs --url URL --out evidence.json [--scene-id ID] [--screenshot screenshot.png] [--width 1440] [--height 900]");
}

function arg(name, fallback = "") {
  const index = process.argv.indexOf(name);
  if (index === -1) return fallback;
  return process.argv[index + 1] ?? fallback;
}

const url = arg("--url");
const out = arg("--out");
if (!url || !out) {
  usage();
  process.exit(2);
}

const width = Number(arg("--width", "1440"));
const height = Number(arg("--height", "900"));
const sceneID = arg("--scene-id", "browser_capture");
const screenshot = arg("--screenshot", "");
const screenshotRef = screenshot ? path.basename(screenshot) : "";

const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width, height } });
  await page.goto(url, { waitUntil: "networkidle" });
  if (screenshot) {
    await page.screenshot({ path: screenshot, fullPage: false });
  }
  const payload = await page.evaluate(({ sceneID, screenshotRef }) => {
    const all = Array.from(document.querySelectorAll("body *"));
    const selected = all.filter((el) => {
      const rect = el.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && (
        el.dataset.visualId ||
        el.dataset.testid ||
        el.getAttribute("data-testid") ||
        el.dataset.visualRole ||
        ["BUTTON", "CANVAS", "IMG", "H1", "H2", "H3", "SECTION", "MAIN", "ASIDE", "NAV"].includes(el.tagName)
      );
    });
    const idFor = (el, index) => el.dataset.visualId || el.dataset.testid || el.getAttribute("data-testid") || el.id || `node-${index}`;
    const idByElement = new Map(selected.map((el, index) => [el, idFor(el, index)]));
    const domDepth = (el) => {
      let depth = 0;
      for (let current = el; current; current = current.parentElement) {
        depth += 1;
      }
      return depth;
    };
    const nearestSelectedParent = (el) => selected
      .filter((candidate) => candidate !== el && candidate.contains(el))
      .sort((a, b) => domDepth(b) - domDepth(a))[0];
    const nodes = selected.map((el, index) => {
      const rect = el.getBoundingClientRect();
      const style = getComputedStyle(el);
      const parent = nearestSelectedParent(el);
      const z = Number.parseInt(style.zIndex, 10);
      const fontSize = Number.parseInt(style.fontSize, 10);
      const opacity = Number.parseFloat(style.opacity);
      return {
        id: idFor(el, index),
        parent_id: parent ? idByElement.get(parent) : undefined,
        test_id: el.dataset.testid || el.getAttribute("data-testid") || undefined,
        role: el.getAttribute("role") || el.dataset.visualRole || undefined,
        accessible_name: el.getAttribute("aria-label") || el.textContent?.trim().slice(0, 120) || undefined,
        text: el.textContent?.trim().slice(0, 120) || undefined,
        tag: el.tagName.toLowerCase(),
        bounds_px: {
          x: Math.round(rect.x),
          y: Math.round(rect.y),
          w: Math.round(rect.width),
          h: Math.round(rect.height)
        },
        styles: {
          display: style.display,
          position: style.position,
          z_index: Number.isFinite(z) ? z : undefined,
          font_size_px: Number.isFinite(fontSize) ? fontSize : undefined,
          font_weight: style.fontWeight,
          background: style.backgroundColor,
          color: style.color,
          opacity: Number.isFinite(opacity) ? opacity : undefined
        },
        attributes: {
          "data-visual-id": el.dataset.visualId || undefined,
          "data-visual-role": el.dataset.visualRole || undefined,
          "data-testid": el.dataset.testid || el.getAttribute("data-testid") || undefined,
          id: el.id || undefined
        }
      };
    });
    return {
      evidence: {
        scene_id: sceneID,
        viewport: {
          width_px: window.innerWidth,
          height_px: window.innerHeight
        },
        screenshot_ref: screenshotRef || undefined,
        nodes
      }
    };
  }, { sceneID, screenshotRef });
  await fs.mkdir(path.dirname(out), { recursive: true });
  await fs.writeFile(out, JSON.stringify(payload, null, 2));
} finally {
  await browser.close();
}
