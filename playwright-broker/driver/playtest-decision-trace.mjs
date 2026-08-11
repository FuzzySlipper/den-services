export class DecisionTrace {
  constructor({ enabled, append }) {
    this.enabled = enabled === true;
    this.append = append;
    this.entries = this.enabled ? [] : undefined;
  }

  async record(request, result, timelineOffset) {
    if (!this.enabled || !request.trace || typeof request.trace !== "object") return null;
    const phase = request.kind === "act" ? "act" : request.kind === "observe" ? "verify" : request.kind;
    const entry = {
      offset: this.entries.length,
      at: new Date().toISOString(),
      cycle_id: request.trace.cycle_id ?? request.trace.cycleId ?? null,
      phase,
      sequence: request.sequence ?? null,
      timeline_offset: timelineOffset,
      summary: jsonSafe(request.trace)
    };
    if (phase === "act") {
      entry.actions = jsonSafe(request.actions || (request.action ? [request.action] : []));
      entry.action_results = jsonSafe(result?.results || []);
    }
    if (phase === "verify") {
      entry.artifacts = [result?.screenshot, ...(result?.frames || [])].filter(Boolean);
    }
    this.entries.push(entry);
    await this.append(entry);
    return entry;
  }
}

function jsonSafe(value) {
  if (value === undefined) return null;
  try {
    return JSON.parse(JSON.stringify(value));
  } catch (error) {
    return { serialization_error: String(error), preview: String(value) };
  }
}
