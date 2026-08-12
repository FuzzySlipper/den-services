import assert from "node:assert/strict";
import test from "node:test";
import {
  completionEvidence,
  retainCompletionEvidence,
  retainStartGuidanceEvidence,
  startGuidanceEvidence,
} from "./playtest-evidence.mjs";

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

test("records the exact supplied field guide and source handles as run input", () => {
  const request = {
    field_guide: {
      guide_id: "fixture/room-one",
      revision: "7",
      sha256: "7".repeat(64),
      notes_markdown: "Press E near the brass switch.",
    },
    source_handles: [
      { kind: "den-knowledge", handle: "gameplay-interaction-completion" },
    ],
  };

  const retained = retainStartGuidanceEvidence({}, request);
  assert.deepEqual(retained, startGuidanceEvidence(request));
  assert.deepEqual(retained.field_guide_input.snapshot, request.field_guide);
  assert.deepEqual(retained.field_guide_input.source_handles, request.source_handles);
});

test("a second run receives only the first run's complete replacement", () => {
  const staleInput = {
    field_guide: {
      guide_id: "fixture/door",
      revision: "1",
      sha256: "1".repeat(64),
      notes_markdown: "The north door opens and is traversable.",
    },
  };
  const firstRun = {};
  retainStartGuidanceEvidence(firstRun, staleInput);
  retainCompletionEvidence(firstRun, {
    outcome: "fail",
    neutral_observation: {
      trajectory: ["The north door moved upward, but forward movement remained blocked."],
    },
    field_guide_usage: {
      contradictions: ["The supplied traversable-door claim contradicted visible movement."],
    },
    field_guide_replacement: {
      schema_version: 1,
      guide_id: "fixture/door",
      revision: "2-candidate",
      replacement_mode: "replace-complete",
      notes_markdown: "Press E to animate the north door. Traversal was blocked in this build.",
      unresolved_questions: ["Is the doorway collider removed after a later event?"],
    },
  });

  const secondRun = {};
  retainStartGuidanceEvidence(secondRun, { field_guide: firstRun.field_guide_replacement });

  assert.equal(secondRun.field_guide_input.snapshot.revision, "2-candidate");
  assert.match(secondRun.field_guide_input.snapshot.notes_markdown, /Press E/);
  assert.match(secondRun.field_guide_input.snapshot.notes_markdown, /Traversal was blocked/);
  assert.doesNotMatch(secondRun.field_guide_input.snapshot.notes_markdown, /opens and is traversable/);
  assert.equal(secondRun.field_guide_input.snapshot.expected_verdict, undefined);
  assert.match(firstRun.neutral_observation.trajectory[0], /movement remained blocked/);
  assert.match(firstRun.field_guide_usage.contradictions[0], /contradicted visible movement/);
});
