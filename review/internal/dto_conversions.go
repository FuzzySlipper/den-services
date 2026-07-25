package review

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
	}
}

func toFinalizationResponse(receipt *ReviewFinalizationReceipt) ReviewFinalizationResponse {
	finalization := receipt.Finalization
	return ReviewFinalizationResponse{
		ID: finalization.ID, ProjectID: finalization.ProjectID, TaskID: finalization.TaskID,
		ReviewRoundID: finalization.ReviewRoundID, Verdict: finalization.Verdict, DecidedBy: finalization.DecidedBy,
		Notes: finalization.Notes, ThreadID: finalization.ThreadID, RunID: finalization.RunID,
		SubagentRole: finalization.SubagentRole, TargetTaskStatus: finalization.TargetTaskStatus,
		IdempotencyKey: finalization.IdempotencyKey, State: finalization.State,
		Packet: toPacketResponse(receipt.Packet), MessageID: finalization.MessageID,
		PacketPostedAt: finalization.PacketPostedAt, TaskTransitionedAt: finalization.TaskTransitionedAt,
		CompletedAt: finalization.CompletedAt, LastErrorStep: finalization.LastErrorStep,
		LastError: finalization.LastError, MessageAttempts: finalization.MessageAttempts,
		TaskTransitionAttempts: finalization.TaskTransitionAttempts, ResultingTaskStatus: receipt.TaskStatus,
		CreatedAt: finalization.CreatedAt, UpdatedAt: finalization.UpdatedAt,
	}
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
