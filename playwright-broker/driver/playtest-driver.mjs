#!/usr/bin/env node

import http from "node:http";
import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { chromium } from "@playwright/test";
import { inputStateDiscrepancies, lifecycleDiscrepancies } from "./playtest-diagnostics.mjs";
import { DecisionTrace } from "./playtest-decision-trace.mjs";
import { isCoordinateClick, startVirtualDisplay, VirtualMouse } from "./playtest-virtual-input.mjs";

const options = JSON.parse(process.env.DEN_PLAYTEST_DRIVER_OPTIONS || "{}");
const artifactRoot = options.artifactRoot;
const indexPath = path.join(artifactRoot, "playtest-index.json");
const timelinePath = path.join(artifactRoot, "timeline.jsonl");
const requestPath = path.join(artifactRoot, "requests.jsonl");
const decisionTracePath = path.join(artifactRoot, "decision-trace.jsonl");
const startedAt = new Date().toISOString();
const events = { console: [], pageErrors: [], requests: [], responses: [], websockets: [] };
const timeline = [];
const discrepancies = [];
const verboseTrace = options.metadata?.verbose_trace === true || options.metadata?.verboseTrace === true;
const decisionTrace = new DecisionTrace({ enabled: verboseTrace, append: entry => appendJSONL(decisionTracePath, entry) });
let nextSequence = 1;
let lastOperation = "start";
let status = "starting";
let finishedAt = null;
let browser;
let context;
let page;
let server;
let virtualDisplay;
let virtualMouse;

const index = {
  schema_version: "den-playwright-playtest/v1",
  session_id: options.sessionId,
  project: options.project,
  repository: options.repoRoot,
  revision: options.revision || {},
  den: options.den || undefined,
  scenario: options.scenario || "",
  owner_label: options.owner || "",
  metadata: options.metadata || {},
  started_at: startedAt,
  finished_at: null,
  status,
  base_url: options.baseURL,
  browser: { name: "chromium", version: "", headed: Boolean(options.headed), virtual_display: false },
  viewport: options.viewport,
  requests_file: requestPath,
  timeline_file: timelinePath,
  timeline: [],
  discrepancies: [],
  observations: [],
  artifacts: [],
  events,
  cleanup: { browser_closed: false, driver_stopped: false }
};
if (verboseTrace) {
  index.decision_trace_file = decisionTracePath;
  index.decision_trace = decisionTrace.entries;
}

await fs.mkdir(artifactRoot, { recursive: true });

function jsonSafe(value) {
  if (value === undefined) return null;
  try {
    return JSON.parse(JSON.stringify(value));
  } catch (error) {
    return { serialization_error: String(error), preview: String(value) };
  }
}

async function appendJSONL(file, value) {
  await fs.appendFile(file, `${JSON.stringify(value)}\n`);
}

async function persist() {
  index.status = status;
  index.finished_at = finishedAt;
  index.timeline = timeline;
  index.discrepancies = discrepancies;
  index.events = events;
  await fs.writeFile(indexPath, `${JSON.stringify(index, null, 2)}\n`);
}

async function collectArtifacts(directory = artifactRoot) {
  const collected = [];
  for (const entry of await fs.readdir(directory, { withFileTypes: true })) {
    const absolute = path.join(directory, entry.name);
    if (entry.isDirectory()) collected.push(...await collectArtifacts(absolute));
    else collected.push(path.relative(artifactRoot, absolute));
  }
  return collected;
}

async function record(kind, data = {}) {
  const entry = { offset: timeline.length, at: new Date().toISOString(), kind, ...jsonSafe(data) };
  timeline.push(entry);
  await appendJSONL(timelinePath, entry);
  return entry;
}

function noteDiscrepancies(request) {
  const result = lifecycleDiscrepancies(request, {
    owner: options.owner,
    nextSequence,
    status,
    lastOperation
  });
  const found = result.found;
  for (const discrepancy of found) {
    const item = { at: new Date().toISOString(), ...discrepancy, continued: true };
    discrepancies.push(item);
  }
  nextSequence = result.nextSequence;
  return found;
}

async function noteInputStateDiscrepancies(request) {
  const hasExpectation = request.expected_focus !== undefined || request.expectedFocus !== undefined ||
    request.expected_pointer_lock !== undefined || request.expectedPointerLock !== undefined;
  if (!hasExpectation) return [];

  const found = [];
  let state;
  try {
    state = await commonState();
  } catch (error) {
    found.push({ code: "input_state_unavailable", error: String(error) });
    discrepancies.push({ at: new Date().toISOString(), ...found[0], continued: true });
    return found;
  }
  found.push(...inputStateDiscrepancies(request, state));
  for (const discrepancy of found) {
    discrepancies.push({ at: new Date().toISOString(), ...discrepancy, continued: true });
  }
  return found;
}

function attachPageEvents(target) {
  target.on("console", async message => {
    const entry = { at: new Date().toISOString(), type: message.type(), text: message.text() };
    events.console.push(entry);
    await record("console", entry);
  });
  target.on("pageerror", async error => {
    const entry = { at: new Date().toISOString(), message: error.message, stack: error.stack || "" };
    events.pageErrors.push(entry);
    await record("page_error", entry);
  });
  target.on("crash", async () => {
    const entry = { at: new Date().toISOString() };
    events.pageErrors.push({ ...entry, message: "page crashed" });
    await record("page_crash", entry);
    if (page === target) page = null;
  });
  target.on("close", () => {
    if (page === target) page = null;
  });
  target.on("request", async request => {
    const entry = {
      at: new Date().toISOString(), method: request.method(), url: request.url(),
      resource_type: request.resourceType(), headers: await request.allHeaders().catch(() => ({})),
      post_data: request.postData()
    };
    events.requests.push(entry);
  });
  target.on("response", async response => {
    const entry = {
      at: new Date().toISOString(), status: response.status(), url: response.url(),
      headers: await response.allHeaders().catch(() => ({}))
    };
    if (options.captureResponseBodies !== false) {
      entry.body = await response.text().catch(error => `[unavailable: ${error}]`);
    }
    events.responses.push(entry);
  });
  target.on("websocket", socket => {
    const entry = { at: new Date().toISOString(), url: socket.url(), frames: [] };
    events.websockets.push(entry);
    socket.on("framereceived", event => entry.frames.push({ at: new Date().toISOString(), direction: "received", payload: String(event.payload) }));
    socket.on("framesent", event => entry.frames.push({ at: new Date().toISOString(), direction: "sent", payload: String(event.payload) }));
    socket.on("socketerror", error => entry.frames.push({ at: new Date().toISOString(), direction: "error", payload: String(error) }));
  });
}

async function ensurePage() {
  if (browser?.isConnected() && page && !page.isClosed()) return [];
  const recovery = [];
  if (!browser?.isConnected()) {
    if (!options.headed && !virtualDisplay) {
      virtualDisplay = await startVirtualDisplay({ viewport: options.viewport, stderr: process.stderr });
      index.browser.virtual_display = Boolean(virtualDisplay);
    }
    const display = virtualDisplay?.display || process.env.DISPLAY;
    browser = await chromium.launch({
      headless: !options.headed && !virtualDisplay,
      env: display ? { ...process.env, DISPLAY: display } : process.env,
      args: virtualDisplay ? ["--window-position=0,0", `--window-size=${options.viewport?.width || 1280},${(options.viewport?.height || 720) + 200}`] : []
    });
    index.browser.version = browser.version();
    context = null;
    page = null;
    recovery.push("browser_relaunched");
  }
  if (!context) {
    const contextOptions = { viewport: options.viewport || { width: 1280, height: 720 } };
    if (options.recordVideo) contextOptions.recordVideo = { dir: path.join(artifactRoot, "video") };
    context = await browser.newContext(contextOptions);
    await context.tracing.start({ screenshots: true, snapshots: true, sources: true });
    recovery.push("context_created");
  }
  page = await context.newPage();
  const display = virtualDisplay?.display || process.env.DISPLAY;
  virtualMouse = display && options.inputHelper ? new VirtualMouse({
    page,
    helperPath: options.inputHelper,
    display,
    viewport: options.viewport || { width: 1280, height: 720 }
  }) : null;
  attachPageEvents(page);
  await page.goto(options.startURL || options.baseURL, { waitUntil: "domcontentloaded" }).catch(async error => {
    await record("navigation_error", { error: String(error), continued: true });
  });
  recovery.push("page_created");
  return recovery;
}

async function commonState() {
  const state = await page.evaluate(() => ({
    focused: document.hasFocus(),
    active_element: document.activeElement ? `${document.activeElement.tagName.toLowerCase()}${document.activeElement.id ? `#${document.activeElement.id}` : ""}` : null,
    pointer_lock: document.pointerLockElement ? `${document.pointerLockElement.tagName.toLowerCase()}${document.pointerLockElement.id ? `#${document.pointerLockElement.id}` : ""}` : null,
    visibility: document.visibilityState,
    scroll: { x: window.scrollX, y: window.scrollY },
    device_pixel_ratio: window.devicePixelRatio
  })).catch(error => ({ evaluation_error: String(error) }));
  return { url: page.url(), title: await page.title().catch(() => ""), viewport: page.viewportSize(), ...state };
}

async function screenshot(label = "observation") {
  const filename = `${String(index.observations.length + 1).padStart(4, "0")}-${label.replace(/[^a-zA-Z0-9._-]+/g, "-")}.png`;
  const destination = path.join(artifactRoot, "screenshots", filename);
  await fs.mkdir(path.dirname(destination), { recursive: true });
  await page.screenshot({ path: destination, fullPage: false });
  const relative = path.relative(artifactRoot, destination);
  index.artifacts.push(relative);
  return relative;
}

async function evaluateExpression(expression, arg) {
  if (typeof expression !== "string") return page.evaluate(expression, arg);
  return page.evaluate(({ source, value }) => {
    const evaluated = (0, eval)(source);
    return typeof evaluated === "function" ? evaluated(value) : evaluated;
  }, { source: expression, value: arg });
}

async function observe(request) {
  const result = { state: await commonState() };
  if (request.screenshot !== false) result.screenshot = await screenshot(request.label || "observe");
  if (request.dom || request.includeDOM) result.dom = await page.content();
  if (request.text || request.includeText) result.text = await page.locator("body").innerText().catch(error => `[unavailable: ${error}]`);
  if (request.accessibility || request.includeAccessibility) {
    result.accessibility = await page.locator("html").ariaSnapshot().catch(error => `[unavailable: ${error}]`);
  }
  if (request.storage || request.includeStorage) {
    result.storage = await page.evaluate(() => ({ localStorage: { ...localStorage }, sessionStorage: { ...sessionStorage }, cookies: document.cookie }));
  }
  if (request.expressions) {
    result.expressions = [];
    for (const expression of request.expressions) {
      result.expressions.push({ expression, value: jsonSafe(await evaluateExpression(expression).catch(error => ({ error: String(error) }))) });
    }
  }
  const frameBurst = request.frameBurst || request.frames;
  if (frameBurst) {
    const count = Number(frameBurst.count ?? frameBurst) || 1;
    const interval = Number(frameBurst.intervalMs ?? 100);
    result.frames = [];
    for (let frame = 0; frame < count; frame += 1) {
      if (frame > 0) await page.waitForTimeout(interval);
      result.frames.push(await screenshot(`${request.label || "burst"}-${frame + 1}`));
    }
  }
  result.event_counts = Object.fromEntries(Object.entries(events).map(([key, value]) => [key, value.length]));
  index.observations.push({ at: new Date().toISOString(), ...jsonSafe(result) });
  return result;
}

async function runAction(action) {
  switch (action.type) {
    case "keyboard_press": return page.keyboard.press(action.key, action.options || {});
    case "keyboard_down": return page.keyboard.down(action.key);
    case "keyboard_up": return page.keyboard.up(action.key);
    case "mouse_move": return virtualMouse ? virtualMouse.move(action.x, action.y, action.options || {}) : page.mouse.move(action.x, action.y, action.options || {});
    case "mouse_click": return virtualMouse ? virtualMouse.click(action.x, action.y, action.options || {}) : page.mouse.click(action.x, action.y, action.options || {});
    case "mouse_down": return virtualMouse ? virtualMouse.down(action.options || {}) : page.mouse.down(action.options || {});
    case "mouse_up": return virtualMouse ? virtualMouse.up(action.options || {}) : page.mouse.up(action.options || {});
    case "mouse_wheel": return page.mouse.wheel(action.deltaX || 0, action.deltaY || 0);
    case "wait": return page.waitForTimeout(action.ms || 0);
    case "viewport": return page.setViewportSize({ width: action.width, height: action.height });
    case "goto": return page.goto(action.url, action.options || {});
    case "click":
      if (isCoordinateClick(action)) return virtualMouse ? virtualMouse.click(action.x, action.y, action.options || {}) : page.mouse.click(action.x, action.y, action.options || {});
      return page.locator(action.selector).click(action.options || {});
    case "dblclick": return page.locator(action.selector).dblclick(action.options || {});
    case "fill": return page.locator(action.selector).fill(action.value, action.options || {});
    case "type": return page.locator(action.selector).pressSequentially(action.text, action.options || {});
    case "focus": return page.locator(action.selector).focus();
    case "hover": return page.locator(action.selector).hover(action.options || {});
    case "select_option": return page.locator(action.selector).selectOption(action.values ?? action.value, action.options || {});
    case "set_input_files": return page.locator(action.selector).setInputFiles(action.files, action.options || {});
    case "dispatch": return page.locator(action.selector).dispatchEvent(action.event, action.eventInit || {});
    case "evaluate": return evaluateExpression(action.expression, action.arg);
    case "evaluate_handle": {
      const handle = await page.evaluateHandle(({ source, value }) => {
        const evaluated = (0, eval)(source);
        return typeof evaluated === "function" ? evaluated(value) : evaluated;
      }, { source: action.expression, value: action.arg });
      const value = await handle.jsonValue().catch(() => String(handle));
      await handle.dispose();
      return value;
    }
    case "add_script": return page.addScriptTag({ content: action.content, path: action.path, url: action.url, type: action.scriptType });
    case "add_style": return page.addStyleTag({ content: action.content, path: action.path, url: action.url });
    case "cdp": {
      const cdp = await context.newCDPSession(page);
      try { return await cdp.send(action.method, action.params || {}); }
      finally { await cdp.detach(); }
    }
    case "request": {
      const method = String(action.method || "get").toLowerCase();
      const response = await context.request[method](action.url, action.options || {});
      return { status: response.status(), headers: response.headers(), body: await response.text() };
    }
    case "screenshot": return screenshot(action.label || "action");
    default:
      if (action.expression) return evaluateExpression(action.expression, action.arg);
      throw new Error(`unknown action type ${JSON.stringify(action.type)}`);
  }
}

async function act(request) {
  const actions = request.actions || (request.action ? [request.action] : []);
  const results = [];
  for (let actionIndex = 0; actionIndex < actions.length; actionIndex += 1) {
    const action = actions[actionIndex];
    try {
      const value = await runAction(action);
      results.push({ action_index: actionIndex, ok: true, value: jsonSafe(value) });
    } catch (error) {
      const diagnostic = { action_index: actionIndex, ok: false, error: String(error), continued: true };
      results.push(diagnostic);
      discrepancies.push({ at: new Date().toISOString(), code: "action_error", ...diagnostic });
    }
  }
  return { results, state: await commonState() };
}

async function inspect(request) {
  const result = { state: await commonState() };
  if (request.expression) result.value = jsonSafe(await evaluateExpression(request.expression, request.arg));
  if (request.selector) {
    result.selector = {
      count: await page.locator(request.selector).count(),
      html: request.html === false ? undefined : await page.locator(request.selector).allInnerTexts().catch(error => [`[unavailable: ${error}]`])
    };
  }
  if (request.cdp) {
    const cdp = await context.newCDPSession(page);
    try { result.cdp = jsonSafe(await cdp.send(request.cdp.method, request.cdp.params || {})); }
    finally { await cdp.detach(); }
  }
  if (request.dom) result.dom = await page.content();
  if (request.events) result.events = events;
  return result;
}

async function finish(request) {
  status = request.kind === "cancel" ? "cancelled" : (request.outcome || "finished");
  if (request.annotation) index.metadata.final_annotation = request.annotation;
  if (request.assertions) index.metadata.assertions = request.assertions;
  const exitInterview = request.exit_interview ?? request.exitInterview ?? request.tester_feedback ?? request.testerFeedback;
  if (exitInterview !== undefined) index.metadata.exit_interview = jsonSafe(exitInterview);
  try {
    await context?.tracing.stop({ path: path.join(artifactRoot, "trace.zip") });
    index.artifacts.push("trace.zip");
  } catch (error) {
    discrepancies.push({ at: new Date().toISOString(), code: "trace_finalize_error", error: String(error), continued: true });
  }
  try {
    await context?.close();
    index.cleanup.browser_closed = true;
  } catch (error) {
    discrepancies.push({ at: new Date().toISOString(), code: "browser_cleanup_error", error: String(error), continued: true });
  }
  try { await browser?.close(); } catch {}
  try { await virtualDisplay?.stop(); index.cleanup.virtual_display_stopped = true; } catch {}
  finishedAt = new Date().toISOString();
  index.cleanup.driver_stopped = true;
  index.artifacts = [...new Set(await collectArtifacts())].sort();
  await persist();
  setTimeout(() => server.close(() => process.exit(0)), 25);
  return { status, index_path: indexPath, cleanup: index.cleanup, exit_interview: index.metadata.exit_interview };
}

async function handle(request) {
  const received = { at: new Date().toISOString(), request: jsonSafe(request) };
  await appendJSONL(requestPath, received);
  const found = noteDiscrepancies(request);
  const recovery = await ensurePage().catch(error => {
    discrepancies.push({ at: new Date().toISOString(), code: "recovery_error", error: String(error), continued: false });
    return [];
  });
  found.push(...await noteInputStateDiscrepancies(request));
  let result;
  try {
    switch (request.kind) {
      case "observe": result = await observe(request); break;
      case "act": result = await act(request); break;
      case "inspect": result = await inspect(request); break;
      case "finish":
      case "cancel": result = await finish(request); break;
      default:
        discrepancies.push({ at: new Date().toISOString(), code: "unknown_operation", received: request.kind, continued: true });
        result = await inspect({ ...request, expression: request.expression });
    }
  } catch (error) {
    result = { partial: true, error: String(error), state: page && !page.isClosed() ? await commonState() : null };
    discrepancies.push({ at: new Date().toISOString(), code: "operation_error", operation: request.kind, error: String(error), continued: true });
  }
  const commandEntry = await record("command", { request, discrepancies: found, recovery, result });
  await decisionTrace.record(request, result, commandEntry.offset);
  await persist();
  lastOperation = request.kind;
  return { ok: !result?.error, continued: true, session_id: options.sessionId, next_sequence: nextSequence, discrepancies: found, recovery, result, index_path: indexPath };
}

async function initialize() {
  await ensurePage();
  status = "running";
  await record("session_started", { url: page.url(), state: await commonState() });
  await persist();
}

server = http.createServer(async (request, response) => {
  response.setHeader("content-type", "application/json");
  if (request.method === "GET" && request.url === "/health") {
    response.end(JSON.stringify({ status, session_id: options.sessionId, browser_connected: Boolean(browser?.isConnected()), page_open: Boolean(page && !page.isClosed()) }));
    return;
  }
  if (request.method !== "POST" || request.url !== "/command") {
    response.statusCode = 404;
    response.end(JSON.stringify({ ok: false, error: "unknown route", continued: true }));
    return;
  }
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  let payload;
  try { payload = JSON.parse(Buffer.concat(chunks).toString("utf8") || "{}"); }
  catch (error) { payload = { kind: "inspect", parse_error: String(error), raw_body: Buffer.concat(chunks).toString("utf8") }; }
  const result = await handle(payload);
  response.end(JSON.stringify(result));
});

process.on("SIGTERM", async () => {
  if (status === "starting" || status === "running") status = "terminated";
  if (!finishedAt) finishedAt = new Date().toISOString();
  try { await context?.tracing.stop({ path: path.join(artifactRoot, "trace.zip") }); } catch {}
  try { await browser?.close(); index.cleanup.browser_closed = true; } catch {}
  try { await virtualDisplay?.stop(); index.cleanup.virtual_display_stopped = true; } catch {}
  index.cleanup.driver_stopped = true;
  await persist().catch(() => {});
  process.exit(0);
});

await initialize().catch(async error => {
  status = "infrastructure_error";
  finishedAt = new Date().toISOString();
  discrepancies.push({ at: finishedAt, code: "startup_error", error: String(error), continued: false });
  await persist();
  try { await virtualDisplay?.stop(); } catch {}
  console.error(error);
  process.exit(1);
});

server.listen(options.port, "127.0.0.1", () => {
  console.log(JSON.stringify({ ready: true, endpoint: `http://127.0.0.1:${options.port}`, index_path: indexPath }));
});
