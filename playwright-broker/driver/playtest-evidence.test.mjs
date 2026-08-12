import assert from "node:assert/strict";
import test from "node:test";
import { completionEvidence, retainCompletionEvidence } from "./playtest-evidence.mjs";

test("preserves a neutral account independently from a contradictory acceptance mapping", () => {
  const metadata = {};
  const request = {
    outcome: "pass",
    neutral_observation: {
      initial: "Three character graphics are visible with their heads below their feet.",
      unexpected: ["All three graphics appear vertically inverted."],
    },
    operational_outcome: {
      status: "completed",
      summary: "The requested visual account was captured.",
    },
    acceptance_mapping: {
      owner: "orchestrator",
      status: "pass",
      criteria: [{ name: "graphics are upright", satisfied: true }],
    },
  };

  const retained = retainCompletionEvidence(metadata, request);

  assert.match(retained.neutral_observation.initial, /heads below their feet/);
  assert.equal(retained.acceptance_mapping.status, "pass");
  assert.deepEqual(metadata, retained);
});

test("derives a minimal operational outcome from the lifecycle outcome", () => {
  assert.deepEqual(completionEvidence({ outcome: "uncertain" }), {
    operational_outcome: { status: "uncertain" },
  });
});
