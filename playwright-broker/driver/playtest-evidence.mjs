function firstDefined(request, ...keys) {
  for (const key of keys) {
    if (request[key] !== undefined) return request[key];
  }
  return undefined;
}

export function completionEvidence(request, jsonSafe = value => value) {
  const evidence = {};
  const neutralObservation = firstDefined(request, "neutral_observation", "neutralObservation");
  const operationalOutcome = firstDefined(request, "operational_outcome", "operationalOutcome");
  const acceptanceMapping = firstDefined(request, "acceptance_mapping", "acceptanceMapping");

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
  return evidence;
}

export function retainCompletionEvidence(metadata, request, jsonSafe = value => value) {
  const evidence = completionEvidence(request, jsonSafe);
  Object.assign(metadata, evidence);
  return evidence;
}
