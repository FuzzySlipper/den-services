package review

import (
	"fmt"
	"strings"
)

func renderReviewRequestPacket(round *ReviewRound, kind string) string {
	var builder strings.Builder
	builder.WriteString("# Review Request\n\n")
	builder.WriteString(fmt.Sprintf("- packet_kind: %s\n", kind))
	builder.WriteString(fmt.Sprintf("- project_id: %s\n", round.ProjectID))
	builder.WriteString(fmt.Sprintf("- task_id: %d\n", round.TaskID))
	builder.WriteString(fmt.Sprintf("- review_round_id: %d\n", round.ID))
	builder.WriteString(fmt.Sprintf("- branch: %s\n", round.Branch))
	builder.WriteString(fmt.Sprintf("- base_branch: %s\n", round.BaseBranch))
	builder.WriteString(fmt.Sprintf("- head_commit: %s\n\n", round.HeadCommit))
	if round.Notes != "" {
		builder.WriteString("## Notes\n\n")
		builder.WriteString(round.Notes)
		builder.WriteString("\n\n")
	}
	return builder.String()
}

func renderVerdictPacket(round *ReviewRound) string {
	var builder strings.Builder
	builder.WriteString("# Review Verdict\n\n")
	builder.WriteString(fmt.Sprintf("- project_id: %s\n", round.ProjectID))
	builder.WriteString(fmt.Sprintf("- task_id: %d\n", round.TaskID))
	builder.WriteString(fmt.Sprintf("- review_round_id: %d\n", round.ID))
	builder.WriteString(fmt.Sprintf("- verdict: %s\n", round.Verdict))
	builder.WriteString(fmt.Sprintf("- decided_by: %s\n\n", round.VerdictBy))
	if round.VerdictNotes != "" {
		builder.WriteString(round.VerdictNotes)
		builder.WriteString("\n")
	}
	return builder.String()
}

func reviewFindingsPacket(round *ReviewRound, findings []*ReviewFinding, openFindings []string, req PostReviewFindingsRequest) *ReviewPacket {
	var builder strings.Builder
	builder.WriteString("Review findings\n\n")
	builder.WriteString(fmt.Sprintf("Review round: `%d`\n", round.RoundNumber))
	builder.WriteString(fmt.Sprintf("Verdict: `%s`\n", firstNonEmpty(round.Verdict, "pending")))
	builder.WriteString(fmt.Sprintf("Reviewed diff: `%s...%s`\n", round.PreferredDiffBaseRef, round.PreferredDiffHeadRef))
	if round.AlternateDiffBaseRef != "" || round.AlternateDiffHeadRef != "" {
		builder.WriteString(fmt.Sprintf("Alternate diff: `%s...%s`\n", round.AlternateDiffBaseRef, round.AlternateDiffHeadRef))
	}
	if round.DeltaBaseCommit != "" {
		builder.WriteString(fmt.Sprintf("Delta since last review: `%s..%s`\n", round.DeltaBaseCommit, round.HeadCommit))
	}

	testCommands := uniqueFindingValues(findings, func(finding *ReviewFinding) []string { return finding.TestCommands })
	appendListSection(&builder, "Reviewer test commands", testCommands, false)

	builder.WriteString("\nFindings:\n")
	if len(findings) == 0 {
		builder.WriteString("- (none)\n")
	} else {
		for _, finding := range findings {
			builder.WriteString("\n")
			builder.WriteString(fmt.Sprintf("%s - %s\n", finding.FindingKey, finding.Category))
			builder.WriteString(fmt.Sprintf("Status: %s\n", finding.Status))
			builder.WriteString(fmt.Sprintf("Summary: %s\n", finding.Summary))
			if len(finding.FileReferences) > 0 {
				builder.WriteString(fmt.Sprintf("Files: %s\n", strings.Join(finding.FileReferences, ", ")))
			}
			if len(finding.TestCommands) > 0 {
				builder.WriteString(fmt.Sprintf("Tests: %s\n", strings.Join(finding.TestCommands, ", ")))
			}
			if finding.Notes != "" {
				builder.WriteString(fmt.Sprintf("Notes: %s\n", collapseWhitespace(finding.Notes)))
			}
		}
	}

	appendListSection(&builder, "Open findings after review", openFindings, false)
	if strings.TrimSpace(req.Notes) != "" {
		builder.WriteString("\nNotes:\n")
		builder.WriteString(fmt.Sprintf("- %s\n", collapseWhitespace(req.Notes)))
	}
	body := strings.TrimSpace(builder.String())
	envelope := metadataForRound(round, PacketKindReviewFindings, metadataTypeForPacket(PacketKindReviewFindings, ""), firstNonEmpty(round.Verdict, VerdictChangesRequested))
	envelope["sender"] = strings.TrimSpace(req.Sender)
	envelope["reviewed_head_commit"] = round.HeadCommit
	envelope["findings"] = findingMetadata(findings)
	envelope["open_findings"] = openFindings
	envelope["reviewer_test_commands"] = testCommands
	if strings.TrimSpace(req.RunID) != "" {
		envelope["run_id"] = strings.TrimSpace(req.RunID)
	}
	if strings.TrimSpace(req.SubagentRole) != "" {
		envelope["subagent_role"] = strings.TrimSpace(req.SubagentRole)
	}
	return &ReviewPacket{
		ProjectID: round.ProjectID, TaskID: round.TaskID, ReviewRoundID: &round.ID, PacketKind: PacketKindReviewFindings,
		Sender: strings.TrimSpace(req.Sender), FrontMatter: envelope, TypedEnvelope: envelope,
		MarkdownBody: body, SourceMarkdown: body, ValidationStatus: PacketStatusValid, CreatedAt: round.UpdatedAt,
	}
}

func unresolvedFindingSummaries(findings []*ReviewFinding) []string {
	result := make([]string, 0, len(findings))
	for _, finding := range findings {
		if resolvedStatus(finding.Status) {
			continue
		}
		line := fmt.Sprintf("%s %s %s %s", finding.FindingKey, finding.Category, finding.Status, finding.Summary)
		if detail := currentFindingDisplayNote(finding); detail != "" {
			line += " (" + collapseWhitespace(detail) + ")"
		}
		result = append(result, line)
	}
	return result
}

func currentFindingDisplayNote(finding *ReviewFinding) string {
	if finding.StatusNotes != "" {
		return finding.StatusNotes
	}
	if finding.ResponseNotes == "" {
		return ""
	}
	if finding.StatusUpdatedAt == nil || finding.ResponseAt != nil && finding.ResponseAt.After(*finding.StatusUpdatedAt) {
		return finding.ResponseNotes
	}
	if finding.Status == StatusClaimedFixed && finding.StatusUpdatedBy == finding.ResponseBy {
		return finding.ResponseNotes
	}
	return ""
}

func uniqueFindingValues(findings []*ReviewFinding, values func(*ReviewFinding) []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, finding := range findings {
		for _, value := range values(finding) {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	return result
}

func findingMetadata(findings []*ReviewFinding) []map[string]any {
	result := make([]map[string]any, 0, len(findings))
	for _, finding := range findings {
		entry := map[string]any{
			"finding_key":         finding.FindingKey,
			"review_round_number": finding.RoundNumber,
			"category":            finding.Category,
			"status":              finding.Status,
			"summary":             finding.Summary,
			"file_references":     finding.FileReferences,
			"test_commands":       finding.TestCommands,
		}
		if finding.FollowUpTaskID != nil {
			entry["follow_up_task_id"] = *finding.FollowUpTaskID
		}
		result = append(result, entry)
	}
	return result
}

func appendListSection(builder *strings.Builder, heading string, items []string, skipIfEmpty bool) {
	if skipIfEmpty && len(items) == 0 {
		return
	}
	builder.WriteString("\n")
	builder.WriteString(heading)
	builder.WriteString(":\n")
	if len(items) == 0 {
		builder.WriteString("- (none recorded)\n")
		return
	}
	for _, item := range items {
		builder.WriteString("- ")
		builder.WriteString(item)
		builder.WriteString("\n")
	}
}

func collapseWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func renderFollowUpDescription(task TaskContext, findings []*ReviewFinding, splitBy string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Follow-up for review findings split from task #%d in project `%s`.\n\n", task.ID, task.ProjectID))
	builder.WriteString(fmt.Sprintf("Split by: %s\n\n", splitBy))
	for _, finding := range findings {
		builder.WriteString(fmt.Sprintf("## %s\n\n", finding.FindingKey))
		builder.WriteString(fmt.Sprintf("- category: %s\n", finding.Category))
		builder.WriteString(fmt.Sprintf("- summary: %s\n", finding.Summary))
		if len(finding.FileReferences) > 0 {
			builder.WriteString(fmt.Sprintf("- file_references: %s\n", strings.Join(finding.FileReferences, ", ")))
		}
		if len(finding.TestCommands) > 0 {
			builder.WriteString(fmt.Sprintf("- test_commands: %s\n", strings.Join(finding.TestCommands, ", ")))
		}
		if finding.Notes != "" {
			builder.WriteString("\n")
			builder.WriteString(finding.Notes)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func toFindingResponses(findings []*ReviewFinding) []ReviewFindingResponse {
	responses := make([]ReviewFindingResponse, 0, len(findings))
	for _, finding := range findings {
		responses = append(responses, toFindingResponse(finding))
	}
	return responses
}
