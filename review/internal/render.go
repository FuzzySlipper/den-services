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
