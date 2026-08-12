#!/usr/bin/env node

import { readFileSync, realpathSync, statSync } from "node:fs";
import { isAbsolute, relative, resolve, sep } from "node:path";

function fail(message) {
  console.error(`product-playtest adoption check failed: ${message}`);
  process.exit(1);
}

function readJSON(path, label) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch (error) {
    fail(`${label} ${path} is not readable JSON: ${error.message}`);
  }
}

const [repoArgument, manifestArgument, scenarioArgument] = process.argv.slice(2);
if (!repoArgument || !manifestArgument || !scenarioArgument) {
  fail("usage: check-product-playtest-adoption.mjs REPO MANIFEST SCENARIO");
}

const repoInput = resolve(repoArgument);

try {
  if (!statSync(repoInput).isDirectory()) {
    fail(`repository path is not a directory: ${repoInput}`);
  }
} catch (error) {
  fail(`repository path is unavailable: ${repoInput}: ${error.message}`);
}

const repo = realpathSync(repoInput);

function resolveOwnedFile(argument, label) {
  const input = resolve(argument);
  let path;
  try {
    if (!statSync(input).isFile()) {
      fail(`${label} path is not a file: ${input}`);
    }
    path = realpathSync(input);
  } catch (error) {
    fail(`${label} path is unavailable: ${input}: ${error.message}`);
  }

  const fromRepo = relative(repo, path);
  const outsideRepo = fromRepo === "" || fromRepo === ".." ||
    fromRepo.startsWith(`..${sep}`) || isAbsolute(fromRepo);
  if (outsideRepo) {
    fail(`${label} must be owned by repository ${repo}: ${path}`);
  }
  return path;
}

const manifestPath = resolveOwnedFile(manifestArgument, "manifest");
const scenarioPath = resolveOwnedFile(scenarioArgument, "scenario");

const manifest = readJSON(manifestPath, "manifest");
const scenario = readJSON(scenarioPath, "scenario");

if (!manifest.project || !manifest.serve?.command) {
  fail("manifest requires project and serve.command");
}
if (!manifest.serve.readyText && !manifest.serve.identityHeader) {
  fail("manifest serve requires readyText or identityHeader");
}
if (!manifest.playtest || !manifest.playtest.startPath) {
  fail("manifest requires playtest.startPath");
}
if (manifest.playtest.viewport?.width <= 0 || manifest.playtest.viewport?.height <= 0) {
  fail("manifest playtest viewport dimensions must be positive when provided");
}
if (![1, 2].includes(scenario.version) || !scenario.project || !scenario.scenario || !scenario.mission) {
  fail("scenario requires version 1 or 2, project, scenario, and mission");
}
if (scenario.project !== manifest.project) {
  fail(`scenario project ${scenario.project} does not match manifest project ${manifest.project}`);
}
if (!Array.isArray(scenario.controls) || scenario.controls.length === 0) {
  fail("scenario requires at least one ordinary control");
}
for (const [index, control] of scenario.controls.entries()) {
  if (!control?.input || !control?.purpose) {
    fail(`scenario control ${index} requires input and purpose`);
  }
}
if (scenario.reproductionLimit !== 1) {
  fail("scenario reproductionLimit must be exactly 1");
}
if (scenario.artifacts?.screenshots !== "before-and-after") {
  fail('scenario artifacts.screenshots must be "before-and-after"');
}
if (!scenario.artifacts?.trace) {
  fail("scenario artifacts.trace must be true");
}
if (scenario.version === 2) {
  if (!scenario.observationProtocol?.initialAccount || !scenario.observationProtocol?.progressAccount) {
    fail("scenario version 2 requires observationProtocol.initialAccount and progressAccount");
  }
  if (!Array.isArray(scenario.observationProtocol?.interactionCompletion) || scenario.observationProtocol.interactionCompletion.length === 0) {
    fail("scenario version 2 requires a non-empty observationProtocol.interactionCompletion");
  }
  if (scenario.orchestratorAcceptance?.owner !== "orchestrator") {
    fail('scenario version 2 requires orchestratorAcceptance.owner to be "orchestrator"');
  }
  if (scenario.orchestratorAcceptance?.withholdUntil !== "after-neutral-account") {
    fail('scenario version 2 requires orchestratorAcceptance.withholdUntil to be "after-neutral-account"');
  }
  if (!Array.isArray(scenario.orchestratorAcceptance?.criteria) || scenario.orchestratorAcceptance.criteria.length === 0) {
    fail("scenario version 2 requires non-empty orchestratorAcceptance.criteria");
  }
  if (!Array.isArray(scenario.orchestratorAcceptance?.insufficientEvidence) || scenario.orchestratorAcceptance.insufficientEvidence.length === 0) {
    fail("scenario version 2 requires non-empty orchestratorAcceptance.insufficientEvidence");
  }
} else {
  console.warn("product-playtest adoption check warning: scenario version 1 is legacy and lacks the observation-first split");
}

console.log(`product-playtest adoption check passed for ${manifest.project}/${scenario.scenario}`);
console.log(`repository: ${repo}`);
console.log(`manifest: ${manifestPath}`);
console.log(`scenario: ${scenarioPath}`);
