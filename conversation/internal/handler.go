package conversation

import (
	"net/http"

	"den-services/shared/api"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/conversation/channels", h.listChannels)
	mux.HandleFunc("POST /v1/conversation/channels", h.createChannel)
	mux.HandleFunc("GET /v1/conversation/channels/{channel_id}", h.getChannel)
	mux.HandleFunc("PUT /v1/conversation/projects/{project_id}/default-channel", h.putProjectDefaultChannel)
	mux.HandleFunc("POST /v1/conversation/channels/{channel_id}/messages", h.appendMessage)
	mux.HandleFunc("GET /v1/conversation/channels/{channel_id}/messages", h.listMessages)
	mux.HandleFunc("PUT /v1/conversation/channels/{channel_id}/memberships", h.putMembership)
	mux.HandleFunc("GET /v1/conversation/memberships", h.listMemberships)
	mux.HandleFunc("POST /v1/conversation/messages/{message_id}/reactions", h.addReaction)
	mux.HandleFunc("GET /v1/conversation/channels/{channel_id}/read-cursors", h.listReadCursors)
	mux.HandleFunc("PUT /v1/conversation/channels/{channel_id}/read-cursors", h.putReadCursor)
}

func (h *Handler) createChannel(w http.ResponseWriter, r *http.Request) {
	var req CreateChannelRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	channel, err := h.service.CreateChannel(r.Context(), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toChannelResponse(channel))
}

func (h *Handler) listChannels(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"), h.service.config)
	if err != nil {
		api.WriteServiceError(w, badRequest(err))
		return
	}
	channels, err := h.service.ListChannels(r.Context(), ListChannelsQuery{
		ProjectID: stringPtrFromQuery(r.URL.Query().Get("project_id")),
		Kind:      stringPtrFromQuery(r.URL.Query().Get("kind")),
		Limit:     limit,
	})
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, channelResponses(channels))
}

func (h *Handler) getChannel(w http.ResponseWriter, r *http.Request) {
	channelID, err := parseRequiredInt64(r.PathValue("channel_id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidChannel))
		return
	}
	channel, err := h.service.GetChannel(r.Context(), channelID)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toChannelResponse(channel))
}

func (h *Handler) putProjectDefaultChannel(w http.ResponseWriter, r *http.Request) {
	var req PutDefaultChannelRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	channel, err := h.service.PutDefaultChannel(r.Context(), r.PathValue("project_id"), req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toChannelResponse(channel))
}

func (h *Handler) appendMessage(w http.ResponseWriter, r *http.Request) {
	channelID, err := parseRequiredInt64(r.PathValue("channel_id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidChannel))
		return
	}
	var req AppendMessageRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	message, err := h.service.AppendMessage(r.Context(), channelID, req, dedupeKeyFromRequest(r, req.DedupeKey))
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toMessageResponse(message))
}

func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) {
	channelID, err := parseRequiredInt64(r.PathValue("channel_id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidChannel))
		return
	}
	afterID, err := parseOptionalInt64(r.URL.Query().Get("after_id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidMessage))
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"), h.service.config)
	if err != nil {
		api.WriteServiceError(w, badRequest(err))
		return
	}
	messages, err := h.service.ListMessages(r.Context(), ListMessagesQuery{
		ChannelID:    channelID,
		AfterID:      afterID,
		AssignmentID: stringPtrFromQuery(r.URL.Query().Get("assignment_id")),
		Limit:        limit,
	})
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, messageResponses(messages))
}

func (h *Handler) putMembership(w http.ResponseWriter, r *http.Request) {
	channelID, err := parseRequiredInt64(r.PathValue("channel_id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidChannel))
		return
	}
	var req PutMembershipRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	membership, err := h.service.PutMembership(r.Context(), channelID, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toMembershipResponse(membership))
}

func (h *Handler) listMemberships(w http.ResponseWriter, r *http.Request) {
	channelID, err := parseOptionalInt64(r.URL.Query().Get("channel_id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidChannel))
		return
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"), h.service.config)
	if err != nil {
		api.WriteServiceError(w, badRequest(err))
		return
	}
	memberships, err := h.service.ListMemberships(r.Context(), ListMembershipsQuery{
		MemberIdentity:    stringPtrFromQuery(r.URL.Query().Get("member_identity")),
		MembershipPurpose: stringPtrFromQuery(r.URL.Query().Get("membership_purpose")),
		ProjectID:         stringPtrFromQuery(r.URL.Query().Get("project_id")),
		ChannelID:         channelID,
		IncludeLeft:       r.URL.Query().Get("include_left") == "true",
		Limit:             limit,
	})
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, membershipResponses(memberships))
}

func (h *Handler) addReaction(w http.ResponseWriter, r *http.Request) {
	messageID, err := parseRequiredInt64(r.PathValue("message_id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidMessage))
		return
	}
	var req AddReactionRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	reaction, err := h.service.AddReaction(r.Context(), messageID, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusCreated, toReactionResponse(reaction))
}

func (h *Handler) listReadCursors(w http.ResponseWriter, r *http.Request) {
	channelID, err := parseRequiredInt64(r.PathValue("channel_id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidChannel))
		return
	}
	cursors, err := h.service.ListReadCursors(r.Context(), channelID)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, cursorResponses(cursors))
}

func (h *Handler) putReadCursor(w http.ResponseWriter, r *http.Request) {
	channelID, err := parseRequiredInt64(r.PathValue("channel_id"))
	if err != nil {
		api.WriteServiceError(w, badRequest(ErrInvalidChannel))
		return
	}
	var req PutReadCursorRequest
	if err := api.DecodeJSON(r, &req); err != nil {
		api.WriteServiceError(w, err)
		return
	}
	cursor, err := h.service.PutReadCursor(r.Context(), channelID, req)
	if err != nil {
		api.WriteServiceError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, toReadCursorResponse(cursor))
}
