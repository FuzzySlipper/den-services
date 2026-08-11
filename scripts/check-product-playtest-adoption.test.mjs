import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";

const checker = new URL("./check-product-playtest-adoption.mjs", import.meta.url).pathname;

function writePacket(repo) {
  const manifest = join(repo, ".den-playwright.json");
  const scenario = join(repo, "product-playtest.scenario.json");
  writeFileSync(manifest, JSON.stringify({
    project: "fixture",
    serve: { command: "fixture --port {port}", readyText: "ready" },
    playtest: { startPath: "/", viewport: { width: 1280, height: 720 } },
  }));
  writeFileSync(scenario, JSON.stringify({
    version: 1,
    project: "fixture",
    scenario: "visible-fixture",
    mission: "Exercise one visible outcome.",
    controls: [{ input: "Click Start", purpose: "Start the fixture" }],
    artifacts: { screenshots: "before-and-after", trace: true },
    reproductionLimit: 1,
  }));
  return { manifest, scenario };
}

test("accepts a packet owned by the claimed repository", () => {
  const root = mkdtempSync(join(tmpdir(), "product-playtest-owned-"));
  try {
    const repo = join(root, "repo");
    mkdirSync(repo);
    const packet = writePacket(repo);
    const result = spawnSync(process.execPath, [checker, repo, packet.manifest, packet.scenario], { encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /adoption check passed/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("rejects cross-repository packet attribution", () => {
  const root = mkdtempSync(join(tmpdir(), "product-playtest-cross-repo-"));
  try {
    const claimedRepo = join(root, "claimed");
    const packetRepo = join(root, "packet-owner");
    mkdirSync(claimedRepo);
    mkdirSync(packetRepo);
    const packet = writePacket(packetRepo);
    const result = spawnSync(process.execPath, [checker, claimedRepo, packet.manifest, packet.scenario], { encoding: "utf8" });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /manifest must be owned by repository/);
    assert.doesNotMatch(result.stdout, /adoption check passed/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
