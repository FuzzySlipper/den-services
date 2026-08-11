import assert from "node:assert/strict";
import test from "node:test";
import { DecisionTrace } from "./playtest-decision-trace.mjs";

test("verbose trace retains ordered act and verify summaries with existing artifacts", async () => {
  const written = [];
  const trace = new DecisionTrace({ enabled: true, append: async entry => written.push(entry) });

  await trace.record({
    kind: "act",
    sequence: 4,
    actions: [{ type: "keyboard_press", key: "w" }],
    trace: {
      cycle_id: "gate-1",
      observe: "A closed gate is centered.",
      hypothesis: "W should approach it.",
      intent: "Walk forward briefly.",
      expected_effect: "The gate grows larger."
    }
  }, { results: [{ action_index: 0, ok: true }] }, 8);
  await trace.record({
    kind: "observe",
    sequence: 5,
    trace: {
      cycle_id: "gate-1",
      observed_effect: "The gate grew larger.",
      matched_expectation: true,
      confidence: "high",
      plan_update: "Try the interaction key."
    }
  }, { screenshot: "screenshots/gate.png", frames: ["screenshots/gate-1.png"] }, 9);

  assert.deepEqual(written.map(entry => [entry.offset, entry.phase, entry.sequence, entry.timeline_offset]), [
    [0, "act", 4, 8],
    [1, "verify", 5, 9]
  ]);
  assert.deepEqual(written[0].actions, [{ type: "keyboard_press", key: "w" }]);
  assert.deepEqual(written[1].artifacts, ["screenshots/gate.png", "screenshots/gate-1.png"]);
  assert.equal(trace.entries[1].summary.plan_update, "Try the interaction key.");
});

test("normal sessions ignore trace-shaped fields without artifacts or entries", async () => {
  let writes = 0;
  const trace = new DecisionTrace({ enabled: false, append: async () => { writes += 1; } });
  const result = await trace.record({ kind: "act", sequence: 1, trace: { cycle_id: "ignored" } }, {}, 0);

  assert.equal(result, null);
  assert.equal(trace.entries, undefined);
  assert.equal(writes, 0);
});
