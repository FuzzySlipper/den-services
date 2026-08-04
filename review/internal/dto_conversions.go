package review

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

func toRoundResponses(rounds []*ReviewRound) []ReviewRoundResponse {
	responses := make([]ReviewRoundResponse, 0, len(rounds))
	for _, round := range rounds {
		responses = append(responses, toRoundResponse(round))
	}
	return responses
}

func toPacketResponse(packet *ReviewPacket) ReviewPacketResponse {
	return ReviewPacketResponse{
		ID: packet.ID, ProjectID: packet.ProjectID, TaskID: packet.TaskID, ReviewRoundID: packet.ReviewRoundID,
		PacketKind: packet.PacketKind, Sender: packet.Sender, MessageID: packet.MessageID,
		FrontMatter: packet.FrontMatter, TypedEnvelope: packet.TypedEnvelope, MarkdownBody: packet.MarkdownBody,
		ValidationStatus: packet.ValidationStatus, ValidationErrors: packet.ValidationErrors,
		CreatedAt: packet.CreatedAt, AcceptedAt: packet.AcceptedAt,
		TaskTransition: packet.TaskTransition, ResultingTaskStatus: packet.ResultingTaskStatus,
	}
}

func toFinalizationResponse(receipt *ReviewFinalizationReceipt) ReviewFinalizationResponse {
	finalization := receipt.Finalization
	response := ReviewFinalizationResponse{
		Schema: ReviewCompletionReceiptSchema, SchemaVersion: 1,
		ID: finalization.ID, ProjectID: finalization.ProjectID, TaskID: finalization.TaskID,
		ReviewRoundID: finalization.ReviewRoundID, Verdict: finalization.Verdict, DecidedBy: finalization.DecidedBy,
		Notes: boundedText(finalization.Notes, 512), ThreadID: finalization.ThreadID, RunID: finalization.RunID,
		SubagentRole: finalization.SubagentRole, TargetTaskStatus: finalization.TargetTaskStatus,
		IdempotencyKey: finalization.IdempotencyKey, MaterialDigest: finalization.MaterialDigest, State: finalization.State,
		PacketID: finalization.PacketID, PacketMessageID: finalization.MessageID, PacketKind: receipt.Packet.PacketKind,
		MessageID:      finalization.MessageID,
		PacketPostedAt: finalization.PacketPostedAt, TaskTransitionedAt: finalization.TaskTransitionedAt,
		CompletedAt: finalization.CompletedAt, LastErrorStep: finalization.LastErrorStep,
		LastError: boundedText(finalization.LastError, 512), MessageAttempts: finalization.MessageAttempts,
		TaskTransitionAttempts: finalization.TaskTransitionAttempts, ResultingTaskStatus: receipt.TaskStatus,
		Reason: finalizationReason(finalization), CreatedAt: finalization.CreatedAt, UpdatedAt: finalization.UpdatedAt,
	}
	if receipt.Round != nil {
		response.ExactHeadCommit = receipt.Round.HeadCommit
	}
	response.FindingStatuses = compactFindingStatuses(receipt.Packet)
	return response
}

func finalizationReason(finalization *ReviewFinalization) string {
	if finalization.LastErrorStep != "" {
		return "review_finalization_" + finalization.LastErrorStep + "_failed"
	}
	if finalization.State == FinalizationStateComplete {
		return "complete"
	}
	return finalization.State
}

func boundedText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	limit -= len("…")
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return value[:limit] + "…"
}

func compactFindingStatuses(packet *ReviewPacket) []FinalizationFindingStatus {
	if packet == nil {
		return nil
	}
	raw, ok := packet.TypedEnvelope["findings"]
	if !ok {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var findings []struct {
		ID         int64  `json:"id"`
		FindingKey string `json:"finding_key"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(data, &findings); err != nil {
		return []FinalizationFindingStatus{{FindingKey: fmt.Sprint(raw), Status: "detail_ref_required"}}
	}
	result := make([]FinalizationFindingStatus, 0, len(findings))
	for _, finding := range findings {
		result = append(result, FinalizationFindingStatus{FindingID: finding.ID, FindingKey: finding.FindingKey, Status: finding.Status})
	}
	return result
}

func toWorkflowSummaryResponse(summary WorkflowSummary) WorkflowSummaryResponse {
	response := WorkflowSummaryResponse{
		CurrentVerdict: summary.CurrentVerdict, ReviewRoundCount: summary.ReviewRoundCount,
		UnresolvedFindingCount: summary.UnresolvedFindingCount, ResolvedFindingCount: summary.ResolvedFindingCount,
		AddressedFindingCount: summary.AddressedFindingCount, OpenFindings: toFindingResponses(summary.OpenFindings),
		ResolvedFindings: toFindingResponses(summary.ResolvedFindings), Timeline: summary.Timeline,
	}
	if summary.CurrentRound != nil {
		round := toRoundResponse(summary.CurrentRound)
		response.CurrentRound = &round
	}
	return response
}

func toGitHubCheckDiscoveryResponse(discovery *GitHubCheckDiscovery) GitHubCheckDiscoveryResponse {
	return GitHubCheckDiscoveryResponse{
		Repository: discovery.Repository, CommitSHA: discovery.CommitSHA,
		RequiredChecks: discovery.RequiredChecks, ConfigurationStatus: discovery.ConfigurationStatus,
		Summary: discovery.Summary, ObservedCheckRuns: discovery.ObservedCheckRuns,
		MissingRequiredChecks:     discovery.MissingRequiredChecks,
		AllObservedChecksTerminal: discovery.AllObservedChecksTerminal,
	}
}
