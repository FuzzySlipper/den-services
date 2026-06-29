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
