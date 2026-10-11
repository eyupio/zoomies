package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/eyupio/zoomies/internal/controller"
)

// assistantChatInput is the body of a chat: the conversation so far, ending in
// the person's question, and optionally which provider to ask. Nothing is kept
// between requests; the page holds the history and sends it again.
type assistantChatInput struct {
	ProviderID string `json:"provider_id"`
	// Page is the path of the page the question was asked from, so the model
	// knows what "here" is. It is a path and nothing else, or empty.
	Page string `json:"page"`
	// Style is how the person wants the answer written, from their settings.
	Style    *assistantStyleInput `json:"style"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// assistantStyleInput is a person's answer style as the UI sends it, with the
// chat and in their preferences. Empty is the default.
type assistantStyleInput struct {
	Length string `json:"length,omitempty"`
	Tone   string `json:"tone,omitempty"`
}

// validate says which part of a style this build does not know, or nothing.
func (in *assistantStyleInput) validate(field string) []fieldError {
	if in == nil {
		return nil
	}
	var errs []fieldError
	if in.Length != "" && !slices.Contains(controller.AssistantLengths, in.Length) {
		errs = append(errs, fieldError{field + ".length", "one of " + strings.Join(controller.AssistantLengths, ", ")})
	}
	if in.Tone != "" && !slices.Contains(controller.AssistantTones, in.Tone) {
		errs = append(errs, fieldError{field + ".tone", "one of " + strings.Join(controller.AssistantTones, ", ")})
	}
	return errs
}

// assistantPageMaxBytes bounds the page path a chat may name: a route of this
// UI is far shorter, and the path goes into the prompt as it is.
const assistantPageMaxBytes = 200

// handleAssistantChat answers a conversation as a stream of Server-Sent Events:
// `delta` frames carrying text, `tool` frames as Eli looks at the fleet (with
// the tool's name and running, done or failed), one `usage` frame when the
// provider reports it, and `done` (saying whether the fleet could be read and
// which tools were used), or `error` if the answer failed after it began.
//
// Anything that can be refused is refused before the stream opens, with a status
// and a code a client can act on, so a stream that opens has an answer coming.
// A failure after that is a frame and not a status, because the status went out
// with the first byte.
func (s *Server) handleAssistantChat(w http.ResponseWriter, r *http.Request) {
	var in assistantChatInput
	if !decode(w, r, &in) {
		return
	}
	if in.Page != "" && (!strings.HasPrefix(in.Page, "/") || len(in.Page) > assistantPageMaxBytes || strings.ContainsAny(in.Page, "\n\r")) {
		unprocessable(w, "page is the path of the page the question is asked from", []fieldError{{"page", "send a path starting with /, at most 200 characters"}})
		return
	}
	if errs := in.Style.validate("style"); len(errs) > 0 {
		unprocessable(w, "style names an answer length or tone this build does not have", errs)
		return
	}
	req := controller.AssistantChatRequest{ProviderID: in.ProviderID, OwnerID: s.assistantOwner(r), AllowSubscription: s.isAssistantAdmin(r), Personal: !s.cfg().Security.DisableAuth && strings.Contains(r.URL.Path, "/assistant/personal/"), Page: in.Page}
	if in.Style != nil {
		req.Style = controller.AssistantStyle{Length: in.Style.Length, Tone: in.Style.Tone}
	}
	// The tools are the person's own: the same routes with the same identity.
	// Whether the provider may be shown the fleet through them is decided by the
	// controller, per provider.
	if id := Identity(r.Context()); id != nil {
		req.Tools = s.assistantToolbox(r, id)
		req.UserID = id.UserID
		req.Role = string(id.Role)
	}
	for _, m := range in.Messages {
		req.Messages = append(req.Messages, controller.AssistantChatMessage{Role: m.Role, Content: m.Content})
	}
	chat, err := s.ctrl.StartAssistantChat(r.Context(), req)
	var invalid *controller.AssistantChatInvalid
	switch {
	case errors.As(err, &invalid):
		unprocessable(w, invalid.Message, []fieldError{{invalid.Field, invalid.Message}})
		return
	case errors.Is(err, controller.ErrAssistantSubscriptionRestricted):
		forbidden(w, err.Error())
		return
	case errors.Is(err, controller.ErrAssistantNotYours):
		writeError(w, http.StatusForbidden, errorEnvelope{Error: errorBody{Code: codeAssistantNotYours, Message: err.Error()}})
		return
	case errors.Is(err, controller.ErrAssistantNoModel):
		conflict(w, "no assistant model is set up; add a provider, test it and make it the default under Settings, Assistant")
		return
	case err != nil:
		// The model's end failed, not this controller. The adapters write their
		// errors for a person and never carry the request or the key.
		writeError(w, http.StatusBadGateway, errorEnvelope{Error: errorBody{Code: codeAssistantProviderFailed, Message: err.Error()}})
		return
	}
	defer chat.Close()

	stream := startSSE(w, r)
	send := func(kind string, v any) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		return stream.event(kind, "", b) == nil
	}
	outcome := "answered"
	// The audit row says who asked which provider to read what, and never what
	// was said: it is written however the answer ends.
	defer func() {
		s.auth.Auditor().Act(r.Context(), Identity(r.Context()), "assistant.chat", "assistant_provider", chat.ProviderID, map[string]any{
			"provider": chat.Provider, "model": chat.Model, "fleet_access": chat.FleetAccess,
			"tools": chat.ToolsUsed(), "outcome": outcome,
			"credentials_hidden_count": chat.Redactions().Credentials, "emails_hidden_count": chat.Redactions().Emails,
		})
	}()
	for {
		ev, ok := chat.Next(r.Context())
		if !ok {
			outcome = "abandoned"
			return
		}
		switch {
		case ev.Err != nil:
			outcome = "failed"
			send("error", map[string]string{"message": ev.Err.Error()})
			return
		case ev.Tool != nil:
			frame := map[string]string{"name": ev.Tool.Name, "status": ev.Tool.Status}
			if ev.Tool.Reason != "" {
				frame["reason"] = ev.Tool.Reason
			}
			if !send("tool", frame) {
				outcome = "abandoned"
				return
			}
		case ev.Usage != nil:
			if !send("usage", map[string]int{"input_tokens": ev.Usage.InputTokens, "output_tokens": ev.Usage.OutputTokens}) {
				outcome = "abandoned"
				return
			}
		case ev.Done:
			hidden := chat.Redactions()
			if ev.Cut {
				outcome = "cut"
			}
			send("done", map[string]any{
				"provider": chat.Provider, "model": chat.Model, "fleet_access": chat.FleetAccess, "tools": chat.ToolsUsed(),
				"redacted": map[string]int{"credentials": hidden.Credentials, "emails": hidden.Emails},
				"cut":      ev.Cut, "provider_id": chat.ProviderID, "output_limit": chat.OutputLimit,
			})
			return
		case ev.Delta != "":
			if !send("delta", map[string]string{"text": ev.Delta}) {
				outcome = "abandoned"
				return
			}
		}
	}
}
