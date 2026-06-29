package review

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func ParseReviewPacketMarkdown(source string) (*ReviewPacket, error) {
	frontMatter, body, err := splitFrontMatter(source)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(frontMatter), &raw); err != nil {
		return nil, validationError(fmt.Errorf("parsing front matter: %w", err), "invalid_front_matter", "front_matter", "common.front_matter")
	}
	if raw == nil {
		return nil, validationError(fmt.Errorf("front matter must be a mapping"), "invalid_front_matter", "front_matter", "common.front_matter")
	}
	packet := &ReviewPacket{
		ProjectID:      stringValue(raw["project_id"]),
		TaskID:         int64Value(raw["task_id"]),
		PacketKind:     stringValue(raw["packet_kind"]),
		Sender:         stringValue(raw["sender"]),
		FrontMatter:    normalizeMap(raw),
		TypedEnvelope:  normalizeMap(raw),
		MarkdownBody:   strings.TrimSpace(body),
		SourceMarkdown: source,
	}
	if packet.PacketKind == "" || !validPacketKind(packet.PacketKind) {
		return nil, validationError(fmt.Errorf("%w: %s", ErrInvalidPacketKind, packet.PacketKind), "invalid_packet_kind", "packet_kind", "common.packet_kind")
	}
	if stringValue(raw["schema"]) != PacketSchema {
		return nil, validationError(fmt.Errorf("schema must be %s", PacketSchema), "invalid_schema", "schema", "common.schema")
	}
	if intValue(raw["schema_version"]) != 1 {
		return nil, validationError(fmt.Errorf("schema_version must be 1"), "invalid_schema_version", "schema_version", "common.schema_version")
	}
	if packet.ProjectID == "" {
		return nil, validationError(ErrMissingProjectID, "missing_project_id", "project_id", "common.project_id")
	}
	if packet.TaskID == 0 {
		return nil, validationError(ErrMissingTaskID, "missing_task_id", "task_id", "common.task_id")
	}
	if packet.Sender == "" {
		return nil, validationError(ErrMissingActor, "missing_sender", "sender", "common.sender")
	}
	if packet.MarkdownBody == "" {
		return nil, validationError(fmt.Errorf("markdown body is required"), "missing_markdown_body", "body", packet.PacketKind+".body")
	}
	if id := int64Value(raw["review_round_id"]); id != 0 {
		packet.ReviewRoundID = &id
	}
	if err := validatePacketFields(packet, raw); err != nil {
		return nil, err
	}
	packet.TypedEnvelope["schema"] = PacketSchema
	packet.TypedEnvelope["schema_version"] = 1
	packet.TypedEnvelope["type"] = metadataTypeForPacket(packet.PacketKind, stringValue(packet.TypedEnvelope["verdict"]))
	packet.TypedEnvelope["packet_kind"] = packet.PacketKind
	return packet, nil
}

func splitFrontMatter(source string) (string, string, error) {
	text := strings.TrimSpace(source)
	if !strings.HasPrefix(text, "---\n") {
		return "", "", validationError(fmt.Errorf("missing YAML front matter"), "missing_front_matter", "front_matter", "common.front_matter")
	}
	rest := strings.TrimPrefix(text, "---\n")
	parts := strings.SplitN(rest, "\n---", 2)
	if len(parts) != 2 {
		return "", "", validationError(fmt.Errorf("unterminated YAML front matter"), "invalid_front_matter", "front_matter", "common.front_matter")
	}
	return parts[0], strings.TrimPrefix(parts[1], "\n"), nil
}

func validatePacketFields(packet *ReviewPacket, raw map[string]any) error {
	if err := verifyChecked(raw, packet.PacketKind); err != nil {
		return err
	}
	switch packet.PacketKind {
	case PacketKindReviewRequest, PacketKindRereviewRequest:
		for _, field := range []string{"requested_by", "branch", "base_branch", "base_commit", "head_commit"} {
			if stringValue(raw[field]) == "" {
				return validationError(fmt.Errorf("%s is required", field), "missing_"+field, field, packet.PacketKind+"."+field)
			}
		}
	case PacketKindReviewFindings:
		if packet.ReviewRoundID == nil {
			return validationError(fmt.Errorf("review_round_id is required"), "missing_review_round_id", "review_round_id", "review_findings.review_round_id")
		}
		if stringValue(raw["reviewed_head_commit"]) == "" {
			return validationError(ErrMissingReviewedCommit, "missing_reviewed_head_commit", "reviewed_head_commit", "review_findings.reviewed_head_commit")
		}
		verdict := stringValue(raw["verdict"])
		if !validVerdict(verdict) {
			return validationError(fmt.Errorf("%w: %s", ErrInvalidVerdict, verdict), "invalid_verdict", "verdict", "review_findings.verdict")
		}
	case PacketKindResponse:
		if packet.ReviewRoundID == nil {
			return validationError(fmt.Errorf("review_round_id is required"), "missing_review_round_id", "review_round_id", "implementer_response.review_round_id")
		}
		if stringValue(raw["reviewed_head_commit"]) == "" {
			return validationError(ErrMissingReviewedCommit, "missing_reviewed_head_commit", "reviewed_head_commit", "implementer_response.reviewed_head_commit")
		}
	case PacketKindCompletion:
		if stringValue(raw["reviewed_head_commit"]) == "" {
			return validationError(ErrMissingReviewedCommit, "missing_reviewed_head_commit", "reviewed_head_commit", "completion_evidence.reviewed_head_commit")
		}
	}
	return nil
}

func verifyChecked(raw map[string]any, kind string) error {
	items, ok := raw["verify"].([]any)
	if !ok || len(items) == 0 {
		return validationError(ErrUncheckedVerify, "missing_verify", "verify", "verify.required_items")
	}
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok || !boolValue(entry["checked"]) {
			return validationError(ErrUncheckedVerify, "unchecked_verify", "verify", "verify.required_items")
		}
	}
	return nil
}

func packetForRound(round *ReviewRound, kind string, _ *int64, runID string) *ReviewPacket {
	body := renderReviewRequestPacket(round, kind)
	envelope := metadataForRound(round, kind, metadataTypeForPacket(kind, ""), "")
	envelope["run_id"] = strings.TrimSpace(runID)
	envelope["packet_kind"] = kind
	return &ReviewPacket{
		ProjectID: round.ProjectID, TaskID: round.TaskID, ReviewRoundID: &round.ID, PacketKind: kind,
		Sender: round.RequestedBy, FrontMatter: envelope, TypedEnvelope: envelope, MarkdownBody: body,
		SourceMarkdown: body, ValidationStatus: PacketStatusValid, IdempotencyKey: fmt.Sprintf("review-round:%d:%s", round.ID, kind),
		CreatedAt: round.CreatedAt,
	}
}

func metadataForRound(round *ReviewRound, packetKind string, metadataType string, verdict string) map[string]any {
	metadata := map[string]any{
		"schema": PacketSchema, "schema_version": 1, "type": metadataType, "packet_kind": packetKind,
		"project_id": round.ProjectID, "task_id": round.TaskID, "review_round_id": round.ID,
		"round_number": round.RoundNumber, "branch": round.Branch, "base_branch": round.BaseBranch,
		"base_commit": round.BaseCommit, "head_commit": round.HeadCommit, "delta_base_commit": round.DeltaBaseCommit,
	}
	if verdict != "" {
		metadata["verdict"] = verdict
	}
	return metadata
}

func metadataTypeForPacket(kind string, verdict string) string {
	switch kind {
	case PacketKindReviewRequest:
		return "review_request_packet"
	case PacketKindRereviewRequest:
		return "rereview_packet"
	case PacketKindReviewFindings:
		return "review_findings_packet"
	case PacketKindCompletion:
		if verdict == VerdictLooksGood {
			return "merge_request"
		}
		return "review_feedback"
	default:
		return kind
	}
}

func verdictType(verdict string) string {
	if verdict == VerdictLooksGood {
		return "merge_request"
	}
	return "review_feedback"
}

func packetKindForVerdict(verdict string) string {
	if verdict == VerdictLooksGood {
		return PacketKindCompletion
	}
	return PacketKindReviewFindings
}

func intentForPacket(kind string, verdict string) string {
	switch kind {
	case PacketKindReviewRequest, PacketKindRereviewRequest:
		return "review_request"
	case PacketKindCompletion:
		if verdict == VerdictLooksGood {
			return "review_approval"
		}
		return "review_feedback"
	default:
		return "review_feedback"
	}
}

func intentForVerdict(verdict string) string {
	if verdict == VerdictLooksGood {
		return "review_approval"
	}
	return "review_feedback"
}

func normalizeMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = normalizeValue(value)
	}
	return result
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return normalizeMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, normalizeValue(item))
		}
		return out
	default:
		return value
	}
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	}
	return 0
}

func intValue(value any) int {
	return int(int64Value(value))
}

func boolValue(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func defaultPacketIdempotencyKey(packet *ReviewPacket) string {
	timestamp := packet.CreatedAt.UnixNano()
	if timestamp == 0 {
		timestamp = time.Now().UnixNano()
	}
	return fmt.Sprintf("review-packet:%s:%d:%s:%d", packet.ProjectID, packet.TaskID, packet.PacketKind, timestamp)
}
