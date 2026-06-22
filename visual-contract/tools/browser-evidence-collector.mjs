#!/usr/bin/env node
import fs from "node:fs/promises";
import path from "node:path";
import { chromium } from "@playwright/test";

function usage() {
  console.error("usage: browser-evidence-collector.mjs --url URL --out evidence.json [--scene-id ID] [--screenshot screenshot.png] [--width 1920] [--height 1080] [--capture-mode viewport-clipped|viewport|page] [--root-selector CSS]");
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

const width = Number(arg("--width", "1920"));
const height = Number(arg("--height", "1080"));
const sceneID = arg("--scene-id", "browser_capture");
const screenshot = arg("--screenshot", "");
const screenshotRef = screenshot ? path.basename(screenshot) : "";
const captureMode = arg("--capture-mode", "viewport-clipped");
const rootSelector = arg("--root-selector", "");
if (!["viewport-clipped", "viewport", "page"].includes(captureMode)) {
  console.error(`unsupported --capture-mode ${captureMode}`);
  usage();
  process.exit(2);
}

const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width, height } });
  await page.goto(url, { waitUntil: "networkidle" });
  if (screenshot) {
    await page.screenshot({ path: screenshot, fullPage: captureMode === "page" });
  }
  const payload = await page.evaluate(({ sceneID, screenshotRef, captureMode, rootSelector }) => {
    const root = rootSelector ? document.querySelector(rootSelector) : document.body;
    if (!root) {
      throw new Error(`root selector did not match: ${rootSelector}`);
    }
    const viewport = {
      left: 0,
      top: 0,
      right: window.innerWidth,
      bottom: window.innerHeight
    };
    const rectIntersectsViewport = (rect) => rect.right > viewport.left &&
      rect.left < viewport.right &&
      rect.bottom > viewport.top &&
      rect.top < viewport.bottom;
    const clippedRect = (rect) => ({
      x: Math.max(viewport.left, rect.x),
      y: Math.max(viewport.top, rect.y),
      w: Math.min(viewport.right, rect.right) - Math.max(viewport.left, rect.x),
      h: Math.min(viewport.bottom, rect.bottom) - Math.max(viewport.top, rect.y)
    });
    const all = [root, ...Array.from(root.querySelectorAll("*"))]
      .filter((el, index, values) => values.indexOf(el) === index && el !== document.body);
    const selected = all.filter((el) => {
      const rect = el.getBoundingClientRect();
      if (rect.width <= 0 || rect.height <= 0) {
        return false;
      }
      if (captureMode !== "page" && !rectIntersectsViewport(rect)) {
        return false;
      }
      return (
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
    const nodeBounds = (rect) => {
      if (captureMode === "page") {
        return {
          bounds: {
            x: Math.round(rect.x + window.scrollX),
            y: Math.round(rect.y + window.scrollY),
            w: Math.round(rect.width),
            h: Math.round(rect.height)
          }
        };
      }
      if (captureMode === "viewport-clipped") {
        const clipped = clippedRect(rect);
        const bounds = {
          x: Math.round(clipped.x),
          y: Math.round(clipped.y),
          w: Math.round(clipped.w),
          h: Math.round(clipped.h)
        };
        const original = {
          x: Math.round(rect.x),
          y: Math.round(rect.y),
          w: Math.round(rect.width),
          h: Math.round(rect.height)
        };
        const wasClipped = bounds.x !== original.x ||
          bounds.y !== original.y ||
          bounds.w !== original.w ||
          bounds.h !== original.h;
        return {
          bounds,
          original_bounds_px: wasClipped ? original : undefined,
          bounds_clipped: wasClipped || undefined
        };
      }
      return {
        bounds: {
          x: Math.round(rect.x),
          y: Math.round(rect.y),
          w: Math.round(rect.width),
          h: Math.round(rect.height)
        }
      };
    };
    const nodes = selected.map((el, index) => {
      const rect = el.getBoundingClientRect();
      const style = getComputedStyle(el);
      const parent = nearestSelectedParent(el);
      const bounds = nodeBounds(rect);
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
        bounds_px: bounds.bounds,
        original_bounds_px: bounds.original_bounds_px,
        bounds_clipped: bounds.bounds_clipped,
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
        coordinate_space: captureMode === "page" ? "page" : "viewport",
        capture_mode: captureMode,
        viewport: {
          width_px: window.innerWidth,
          height_px: window.innerHeight
        },
        page_size: {
          width_px: Math.max(document.documentElement.scrollWidth, document.body.scrollWidth),
          height_px: Math.max(document.documentElement.scrollHeight, document.body.scrollHeight)
        },
        scroll_offset: {
          x_px: window.scrollX,
          y_px: window.scrollY
        },
        screenshot_ref: screenshotRef || undefined,
        nodes
      }
    };
  }, { sceneID, screenshotRef, captureMode, rootSelector });
  await fs.mkdir(path.dirname(out), { recursive: true });
  await fs.writeFile(out, JSON.stringify(payload, null, 2));
} finally {
  await browser.close();
}
