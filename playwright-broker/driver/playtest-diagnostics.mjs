export function lifecycleDiscrepancies(request, state) {
  const found = [];
  if (request.owner && state.owner && request.owner !== state.owner) {
    found.push({ code: "owner_mismatch", expected: state.owner, received: request.owner });
  }
  if (request.sequence === undefined || request.sequence === null) {
    found.push({ code: "sequence_missing", expected: state.nextSequence });
  } else if (request.sequence !== state.nextSequence) {
    found.push({
      code: request.sequence < state.nextSequence ? "sequence_stale" : "sequence_gap",
      expected: state.nextSequence,
      received: request.sequence
    });
  }
  if (state.status !== "running" && request.kind !== "finish" && request.kind !== "cancel") {
    found.push({ code: "unexpected_session_state", state: state.status, requested_kind: request.kind });
  }
  const expectedPreviousKind = request.expected_previous_kind ?? request.expectedPreviousKind;
  if (expectedPreviousKind && expectedPreviousKind !== state.lastOperation) {
    found.push({
      code: "unexpected_operation_order",
      expected_previous_kind: expectedPreviousKind,
      previous_kind: state.lastOperation,
      requested_kind: request.kind
    });
  }
  const numericSequence = Number(request.sequence);
  const nextSequence = Number.isFinite(numericSequence)
    ? Math.max(state.nextSequence + 1, numericSequence + 1)
    : state.nextSequence + 1;
  return { found, nextSequence };
}

export function inputStateDiscrepancies(request, state) {
  const expectedFocus = request.expected_focus ?? request.expectedFocus;
  const expectedPointerLock = request.expected_pointer_lock ?? request.expectedPointerLock;
  const found = [];
  if (expectedFocus !== undefined && Boolean(expectedFocus) !== Boolean(state.focused)) {
    found.push({
      code: expectedFocus ? "focus_inactive" : "focus_unexpected",
      expected: Boolean(expectedFocus),
      observed: Boolean(state.focused),
      active_element: state.active_element
    });
  }
  const pointerLocked = Boolean(state.pointer_lock);
  if (expectedPointerLock !== undefined && Boolean(expectedPointerLock) !== pointerLocked) {
    found.push({
      code: expectedPointerLock ? "pointer_lock_inactive" : "pointer_lock_unexpected",
      expected: Boolean(expectedPointerLock),
      observed: pointerLocked,
      pointer_lock: state.pointer_lock
    });
  }
  return found;
}
