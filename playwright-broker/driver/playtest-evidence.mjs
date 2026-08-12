function firstDefined(request, ...keys) {
  for (const key of keys) {
    if (request[key] !== undefined) return request[key];
  }
  return undefined;
}

export function startGuidanceEvidence(request, jsonSafe = value => value) {
  const snapshot = firstDefined(request, "field_guide", "fieldGuide");
  const sourceHandles = firstDefined(request, "source_handles", "sourceHandles", "knowledge_handles", "knowledgeHandles");
  if (snapshot === undefined && sourceHandles === undefined) return {};

  return {
    field_guide_input: {
      snapshot: snapshot === undefined ? null : jsonSafe(snapshot),
      source_handles: sourceHandles === undefined ? [] : jsonSafe(sourceHandles),
    },
  };
}

export function retainStartGuidanceEvidence(metadata, request, jsonSafe = value => value) {
  const evidence = startGuidanceEvidence(request, jsonSafe);
  Object.assign(metadata, evidence);
  return evidence;
}

export function completionEvidence(request, jsonSafe = value => value) {
  const evidence = {};
  const neutralObservation = firstDefined(request, "neutral_observation", "neutralObservation");
  const operationalOutcome = firstDefined(request, "operational_outcome", "operationalOutcome");
  const acceptanceMapping = firstDefined(request, "acceptance_mapping", "acceptanceMapping");
  const fieldGuideUsage = firstDefined(request, "field_guide_usage", "fieldGuideUsage");
  const fieldGuideReplacement = firstDefined(request, "field_guide_replacement", "fieldGuideReplacement", "next_field_guide", "nextFieldGuide");

  if (neutralObservation !== undefined) {
    evidence.neutral_observation = jsonSafe(neutralObservation);
  }
  if (operationalOutcome !== undefined) {
    evidence.operational_outcome = jsonSafe(operationalOutcome);
  } else if (request.outcome !== undefined) {
    evidence.operational_outcome = jsonSafe({ status: request.outcome });
  }
  if (acceptanceMapping !== undefined) {
    evidence.acceptance_mapping = jsonSafe(acceptanceMapping);
  }
  if (fieldGuideUsage !== undefined) {
    evidence.field_guide_usage = jsonSafe(fieldGuideUsage);
  }
  if (fieldGuideReplacement !== undefined) {
    evidence.field_guide_replacement = jsonSafe(fieldGuideReplacement);
  }
  return evidence;
}

export function retainCompletionEvidence(metadata, request, jsonSafe = value => value) {
  const evidence = completionEvidence(request, jsonSafe);
  Object.assign(metadata, evidence);
  return evidence;
}
