package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"den-services/mcp/internal/backend"
	"den-services/mcp/internal/registry"
)

const detailReferenceVersion = "1"

var (
	errInvalidDetailReference = errors.New("invalid detail reference")
	errExpiredDetailReference = errors.New("expired detail reference")
)

type detailReference struct {
	Version   string                     `json:"version"`
	Tool      string                     `json:"tool"`
	Arguments map[string]json.RawMessage `json:"arguments"`
	IssuedAt  time.Time                  `json:"issued_at"`
}

type getDetailsArguments struct {
	DetailRef string `json:"detail_ref"`
}

func (h *Handler) attachDetailReference(toolName string, arguments, result json.RawMessage) (json.RawMessage, error) {
	if !registry.SupportsDetails(toolName) || requestsVerbose(arguments) {
		return result, nil
	}
	if toolName == "get_review_context" {
		return h.attachReviewContextDetailReferences(arguments, result)
	}
	reference, err := h.newDetailReference(toolName, arguments)
	if err != nil {
		return nil, err
	}
	var toolResult toolsCallResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return nil, fmt.Errorf("decoding tool result for detail reference: %w", err)
	}
	structured := make(map[string]json.RawMessage)
	if len(toolResult.StructuredContent) > 0 {
		if err := json.Unmarshal(toolResult.StructuredContent, &structured); err != nil {
			return nil, fmt.Errorf("decoding structured content for detail reference: %w", err)
		}
	}
	encodedReference, err := json.Marshal(reference)
	if err != nil {
		return nil, fmt.Errorf("encoding detail reference field: %w", err)
	}
	structured["detail_ref"] = encodedReference
	toolResult.StructuredContent, err = json.Marshal(structured)
	if err != nil {
		return nil, fmt.Errorf("encoding structured content with detail reference: %w", err)
	}
	toolResult.Content = append(toolResult.Content, textContent{Type: "text", Text: string(encodedReference)})
	updated, err := json.Marshal(toolResult)
	if err != nil {
		return nil, fmt.Errorf("encoding tool result with detail reference: %w", err)
	}
	return updated, nil
}

func (h *Handler) attachReviewContextDetailReferences(arguments, result json.RawMessage) (json.RawMessage, error) {
	var toolResult toolsCallResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return nil, fmt.Errorf("decoding review context result for detail references: %w", err)
	}
	structured := make(map[string]json.RawMessage)
	if err := json.Unmarshal(toolResult.StructuredContent, &structured); err != nil {
		return nil, fmt.Errorf("decoding review context structured content for detail references: %w", err)
	}
	references, err := h.newReviewContextDetailReferences(arguments, structured)
	if err != nil {
		return nil, err
	}
	if err := replaceReviewContextDetailRefs(structured, references); err != nil {
		return nil, err
	}
	for index := range toolResult.Content {
		if toolResult.Content[index].Type != "text" {
			continue
		}
		updatedText, err := replaceReviewContextDetailRefsInText(toolResult.Content[index].Text, references)
		if err != nil {
			return nil, err
		}
		toolResult.Content[index].Text = updatedText
	}
	encodedStructured, err := json.Marshal(structured)
	if err != nil {
		return nil, fmt.Errorf("encoding review context structured content with detail references: %w", err)
	}
	toolResult.StructuredContent = encodedStructured
	updated, err := json.Marshal(toolResult)
	if err != nil {
		return nil, fmt.Errorf("encoding review context tool result with detail references: %w", err)
	}
	return updated, nil
}

func (h *Handler) newReviewContextDetailReferences(arguments json.RawMessage, structured map[string]json.RawMessage) (map[string]string, error) {
	references := make(map[string]string)
	if raw, ok := structured["detail_refs"]; ok {
		var advertised map[string]string
		if err := json.Unmarshal(raw, &advertised); err != nil {
			return nil, fmt.Errorf("decoding review context advertised detail refs: %w", err)
		}
		for section, handle := range advertised {
			if strings.TrimSpace(handle) == "" {
				continue
			}
			reference, err := h.newReviewContextDetailReference(arguments, section)
			if err != nil {
				return nil, err
			}
			references[section] = reference
		}
	}
	if raw, ok := structured["current_round"]; ok {
		var round map[string]json.RawMessage
		if err := json.Unmarshal(raw, &round); err != nil {
			return nil, fmt.Errorf("decoding review context current round for detail references: %w", err)
		}
		if _, ok := round["campaign_detail_ref"]; ok {
			reference, err := h.newReviewContextDetailReference(arguments, "campaign")
			if err != nil {
				return nil, err
			}
			references["campaign"] = reference
		}
	}
	return references, nil
}

func (h *Handler) newReviewContextDetailReference(rawArguments json.RawMessage, section string) (string, error) {
	arguments := make(map[string]json.RawMessage)
	if err := json.Unmarshal(rawArguments, &arguments); err != nil {
		return "", fmt.Errorf("decoding review context source arguments: %w", err)
	}
	encodedSection, err := json.Marshal(section)
	if err != nil {
		return "", fmt.Errorf("encoding review context detail section: %w", err)
	}
	arguments["detail_section"] = encodedSection
	encodedArguments, err := json.Marshal(arguments)
	if err != nil {
		return "", fmt.Errorf("encoding review context detail arguments: %w", err)
	}
	return h.newDetailReference("get_review_context", encodedArguments)
}

func replaceReviewContextDetailRefs(structured map[string]json.RawMessage, references map[string]string) error {
	if raw, ok := structured["detail_refs"]; ok {
		var refs map[string]string
		if err := json.Unmarshal(raw, &refs); err != nil {
			return fmt.Errorf("decoding review context detail refs: %w", err)
		}
		for key := range refs {
			if reference, ok := references[key]; ok {
				refs[key] = reference
			}
		}
		encoded, err := json.Marshal(refs)
		if err != nil {
			return fmt.Errorf("encoding review context detail refs: %w", err)
		}
		structured["detail_refs"] = encoded
	}
	if findings, ok := structured["prior_findings"]; ok && references["findings"] != "" {
		updatedFindings, err := replaceReviewContextFindingRefs(findings, references["findings"])
		if err != nil {
			return err
		}
		structured["prior_findings"] = updatedFindings
	}
	if currentRound, ok := structured["current_round"]; ok {
		updatedRound, err := replaceReviewContextCampaignRef(currentRound, references["campaign"])
		if err != nil {
			return err
		}
		structured["current_round"] = updatedRound
	}
	return nil
}

func replaceReviewContextCampaignRef(raw json.RawMessage, reference string) (json.RawMessage, error) {
	var round map[string]json.RawMessage
	if err := json.Unmarshal(raw, &round); err != nil {
		return nil, fmt.Errorf("decoding review context campaign ref: %w", err)
	}
	if _, ok := round["campaign_detail_ref"]; !ok {
		return raw, nil
	}
	encoded, err := json.Marshal(reference)
	if err != nil {
		return nil, fmt.Errorf("encoding review context campaign ref: %w", err)
	}
	round["campaign_detail_ref"] = encoded
	updated, err := json.Marshal(round)
	if err != nil {
		return nil, fmt.Errorf("encoding review context campaign ref: %w", err)
	}
	return updated, nil
}

func replaceReviewContextFindingRefs(raw json.RawMessage, reference string) (json.RawMessage, error) {
	var findings []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &findings); err != nil {
		return nil, fmt.Errorf("decoding review context finding refs: %w", err)
	}
	encodedReference, err := json.Marshal(reference)
	if err != nil {
		return nil, fmt.Errorf("encoding review context finding ref: %w", err)
	}
	for _, finding := range findings {
		if _, ok := finding["detail_ref"]; ok {
			finding["detail_ref"] = encodedReference
		}
	}
	updated, err := json.Marshal(findings)
	if err != nil {
		return nil, fmt.Errorf("encoding review context finding refs: %w", err)
	}
	return updated, nil
}

func replaceReviewContextDetailRefsInText(raw string, references map[string]string) (string, error) {
	var structured map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &structured); err != nil {
		return raw, nil
	}
	if err := replaceReviewContextDetailRefs(structured, references); err != nil {
		return "", err
	}
	updated, err := json.Marshal(structured)
	if err != nil {
		return "", fmt.Errorf("encoding review context content detail refs: %w", err)
	}
	return string(updated), nil
}

func (h *Handler) newDetailReference(toolName string, rawArguments json.RawMessage) (string, error) {
	arguments := make(map[string]json.RawMessage)
	if len(rawArguments) > 0 {
		if err := json.Unmarshal(rawArguments, &arguments); err != nil {
			return "", fmt.Errorf("decoding detail source arguments: %w", err)
		}
	}
	safeArguments := make(map[string]json.RawMessage)
	for name, value := range arguments {
		if name == "verbose" {
			continue
		}
		if !registry.DetailArgumentAllowed(toolName, name) {
			return "", fmt.Errorf("%w: argument %s is not safe for %s", errInvalidDetailReference, name, toolName)
		}
		safeArguments[name] = value
	}
	payload, err := json.Marshal(detailReference{
		Version: detailReferenceVersion, Tool: toolName, Arguments: safeArguments, IssuedAt: h.clock().UTC(),
	})
	if err != nil {
		return "", fmt.Errorf("encoding detail reference: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signature := hmac.New(sha256.New, h.detailReferenceKey)
	_, _ = signature.Write([]byte(encodedPayload))
	return "d1." + encodedPayload + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil)), nil
}

func (h *Handler) resolveDetailReference(raw string) (backend.ToolCall, error) {
	if !strings.HasPrefix(raw, "d1.") {
		return backend.ToolCall{}, errInvalidDetailReference
	}
	parts := strings.Split(strings.TrimPrefix(raw, "d1."), ".")
	if len(parts) != 2 {
		return backend.ToolCall{}, errInvalidDetailReference
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return backend.ToolCall{}, errInvalidDetailReference
	}
	expectedSignature := hmac.New(sha256.New, h.detailReferenceKey)
	_, _ = expectedSignature.Write([]byte(parts[0]))
	if !hmac.Equal(providedSignature, expectedSignature.Sum(nil)) {
		return backend.ToolCall{}, errInvalidDetailReference
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return backend.ToolCall{}, errInvalidDetailReference
	}
	var reference detailReference
	if err := json.Unmarshal(payload, &reference); err != nil {
		return backend.ToolCall{}, errInvalidDetailReference
	}
	if reference.Version != detailReferenceVersion || !registry.SupportsDetails(reference.Tool) || reference.IssuedAt.IsZero() {
		return backend.ToolCall{}, errInvalidDetailReference
	}
	if h.clock().UTC().Sub(reference.IssuedAt) > h.detailReferenceTTL || reference.IssuedAt.After(h.clock().UTC().Add(time.Minute)) {
		return backend.ToolCall{}, errExpiredDetailReference
	}
	for name := range reference.Arguments {
		if !registry.DetailArgumentAllowed(reference.Tool, name) {
			return backend.ToolCall{}, errInvalidDetailReference
		}
	}
	reference.Arguments["verbose"] = json.RawMessage(`true`)
	arguments, err := json.Marshal(reference.Arguments)
	if err != nil {
		return backend.ToolCall{}, errInvalidDetailReference
	}
	tool, err := h.registry.Resolve(reference.Tool)
	if err != nil || tool.TombstoneMessage != "" || tool.Operation == "get_details" {
		return backend.ToolCall{}, errInvalidDetailReference
	}
	return backend.ToolCall{ToolName: tool.Name, Operation: tool.Operation, Arguments: arguments}, nil
}

func requestsVerbose(raw json.RawMessage) bool {
	var arguments struct {
		Verbose bool `json:"verbose"`
	}
	return json.Unmarshal(raw, &arguments) == nil && arguments.Verbose
}
