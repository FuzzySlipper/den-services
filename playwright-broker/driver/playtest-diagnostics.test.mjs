import assert from "node:assert/strict";
import test from "node:test";
import { inputStateDiscrepancies, lifecycleDiscrepancies } from "./playtest-diagnostics.mjs";

test("ordinary bad order is advisory and advances sequence", () => {
  const result = lifecycleDiscrepancies({
    kind: "act",
    sequence: 1,
    expected_previous_kind: "observe"
  }, {
    owner: "",
    nextSequence: 1,
    status: "running",
    lastOperation: "start"
  });

  assert.equal(result.nextSequence, 2);
  assert.deepEqual(result.found, [{
    code: "unexpected_operation_order",
    expected_previous_kind: "observe",
    previous_kind: "start",
    requested_kind: "act"
  }]);
});

test("declared inactive focus and pointer lock are distinct diagnostics", () => {
  const result = inputStateDiscrepancies({
    expected_focus: true,
    expected_pointer_lock: true
  }, {
    focused: false,
    active_element: "body",
    pointer_lock: null
  });

  assert.deepEqual(result.map(item => item.code), ["focus_inactive", "pointer_lock_inactive"]);
});
