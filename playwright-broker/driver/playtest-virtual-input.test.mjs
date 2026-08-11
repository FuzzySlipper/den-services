import assert from "node:assert/strict";
import test from "node:test";
import process from "node:process";
import { chromium } from "@playwright/test";
import { isCoordinateClick, startVirtualDisplay, VirtualMouse } from "./playtest-virtual-input.mjs";

test("coordinate click shorthand is distinguished from locator click", () => {
  assert.equal(isCoordinateClick({ type: "click", x: 640, y: 360 }), true);
  assert.equal(isCoordinateClick({ type: "click", selector: "canvas" }), false);
  assert.equal(isCoordinateClick({ type: "mouse_click", x: 640, y: 360 }), false);
});

test("virtual mouse preserves absolute coordinates then emits relative pointer-lock deltas", async () => {
  let locked = false;
  const commands = [];
  const page = { evaluate: async callback => locked ? true : callback.toString().includes("screenX") ? { x: 7, y: 29 } : false };
  const mouse = new VirtualMouse({
    page,
    helperPath: "/helper",
    display: ":42",
    viewport: { width: 1280, height: 720 },
    exec: async (file, args, options) => commands.push({ file, args, display: options.env.DISPLAY })
  });

  await mouse.move(590, 360);
  locked = true;
  await mouse.move(640, 310);

  assert.deepEqual(commands, [
    { file: "/helper", args: ["move-absolute", "597", "389"], display: ":42" },
    { file: "/helper", args: ["move-relative", "50", "-50"], display: ":42" }
  ]);
});

test("virtual mouse uses genuine button events without a pointer-lock reposition", async () => {
  const commands = [];
  const page = { evaluate: async () => true };
  const mouse = new VirtualMouse({
    page,
    helperPath: "/helper",
    display: ":9",
    viewport: { width: 1280, height: 720 },
    exec: async (_file, args) => commands.push(args)
  });

  await mouse.move(590, 360);
  commands.length = 0;
  await mouse.click(640, 360, { button: "right" });

  assert.deepEqual(commands, [
    ["button", "3", "down"],
    ["button", "3", "up"]
  ]);
});

test("X11 input delivers bounded relative movement through Chromium pointer lock", {
  skip: !process.env.DEN_PLAYTEST_INPUT_HELPER
}, async () => {
  const viewport = { width: 1280, height: 720 };
  const display = await startVirtualDisplay({ viewport, stderr: process.stderr });
  let browser;
  try {
    browser = await chromium.launch({
      headless: false,
      env: { ...process.env, DISPLAY: display.display },
      args: ["--window-position=0,0", "--window-size=1280,920"]
    });
    const page = await browser.newPage({ viewport });
    await page.setContent(`<canvas width="1280" height="720"></canvas><script>
      window.moves = [];
      window.buttons = [];
      const canvas = document.querySelector("canvas");
      canvas.addEventListener("click", () => canvas.requestPointerLock());
      document.addEventListener("mousemove", event => moves.push([event.movementX, event.movementY]));
      document.addEventListener("mousedown", event => buttons.push([event.button, "down"]));
      document.addEventListener("mouseup", event => buttons.push([event.button, "up"]));
    </script>`);
    const mouse = new VirtualMouse({
      page,
      helperPath: process.env.DEN_PLAYTEST_INPUT_HELPER,
      display: display.display,
      viewport
    });

    // Playwright acquires lock here so this test remains independent of host
    // window-manager focus. The broker's end-to-end pilot covers XTest click.
    await page.mouse.click(640, 360);
    await page.waitForFunction(() => document.pointerLockElement !== null);
    await page.waitForTimeout(100);
    await page.evaluate(() => { moves.length = 0; });
    await mouse.move(590, 360);
    await page.waitForFunction(() => moves.length > 0);
    const moves = await page.evaluate(() => window.moves);
    await page.evaluate(() => { moves.length = 0; });
    await mouse.click(640, 360, { button: "right" });

    const clickMoves = await page.evaluate(() => window.moves);
    const buttons = await page.evaluate(() => window.buttons);
    assert.equal(moves.reduce((sum, [x]) => sum + x, 0), -50);
    assert.ok(Math.abs(moves.reduce((sum, [, y]) => sum + y, 0)) <= 20, JSON.stringify(moves));
    assert.ok(moves.every(([x, y]) => Math.abs(x) <= 50 && Math.abs(y) <= 50), JSON.stringify(moves));
    assert.deepEqual(clickMoves, []);
    assert.deepEqual(buttons.slice(-2), [[2, "down"], [2, "up"]]);
  } finally {
    await browser?.close();
    await display?.stop();
  }
});
