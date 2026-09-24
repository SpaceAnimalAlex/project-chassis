package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/web/stream"
)

type voiceIncomingRequest struct {
	EventType    string `json:"event_type"` // "call" (default / ringing) or "voicemail"
	CallID       string `json:"call_id"`
	CallerNumber string `json:"caller_number"`
	CallerName   string `json:"caller_name"`
	Subject      string `json:"subject,omitempty"`
	Transcript   string `json:"transcript,omitempty"`
	RecordingURL string `json:"recording_url,omitempty"`
	AudioBase64  string `json:"audio_base64,omitempty"`
	AudioMime    string `json:"audio_mime,omitempty"`
}

// VoiceIncoming handles POST /api/webhooks/voice/incoming — the generic,
// provider-agnostic telephony ingress from the Round 3 design (an on-prem
// PBX dialplan curl / FreePBX AMI daemon, or a cloud SIP trunk normalized to
// this shape upstream).
//
// It serves two operational paths based on the payload:
//  1. Live ringing / Caller ID: resolves the caller against contacts and
//     organizations and broadcasts a screen-pop to every connected operator.
//  2. Voicemail ingestion: ingests audio/transcript via core.InboundIngestor,
//     leveraging Stage 0 phone-to-ticket correlation to attach to an open
//     ticket or open a fresh work item with SSE broadcast and audit logging.
//
// Authenticated by a shared secret (X-Webhook-Secret), not a session cookie
// — the caller is a telephony system, not a signed-in operator, and
// middleware.PublicPath exempts this path from the cookie check accordingly.
func (h *Handlers) VoiceIncoming(w http.ResponseWriter, r *http.Request) {
	if h.VoiceWebhookSecret == "" || r.Header.Get("X-Webhook-Secret") != h.VoiceWebhookSecret {
		writeError(w, http.StatusUnauthorized, "invalid or missing webhook secret")
		return
	}

	var req voiceIncomingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CallerNumber == "" {
		writeError(w, http.StatusBadRequest, "caller_number is required")
		return
	}

	number := core.NormalizePhone(req.CallerNumber)

	// Path 2: Voicemail ingestion (audio transcript / recording drop)
	isVoicemail := req.EventType == "voicemail" || req.Transcript != "" || req.RecordingURL != "" || req.AudioBase64 != ""
	if isVoicemail {
		if h.Ingestor == nil {
			writeError(w, http.StatusNotImplemented, "voicemail ingestion not configured")
			return
		}

		callID := req.CallID
		if callID == "" {
			callID = fmt.Sprintf("vm-%d", time.Now().UnixNano())
		}

		subject := req.Subject
		if subject == "" {
			if req.CallerName != "" {
				subject = fmt.Sprintf("Voicemail from %s (%s)", req.CallerName, number)
			} else {
				subject = fmt.Sprintf("Voicemail from %s", number)
			}
		}

		body := strings.TrimSpace(req.Transcript)
		if req.RecordingURL != "" {
			if body != "" {
				body += "\n\nRecording: " + req.RecordingURL
			} else {
				body = "Recording: " + req.RecordingURL
			}
		}
		if body == "" {
			body = "(Voicemail audio attached)"
		}

		var attachments []core.Attachment
		if req.AudioBase64 != "" {
			data, err := base64.StdEncoding.DecodeString(req.AudioBase64)
			if err == nil && len(data) > 0 {
				mime := req.AudioMime
				if mime == "" {
					mime = "audio/wav"
				}
				ext := ".wav"
				if strings.Contains(mime, "mpeg") || strings.Contains(mime, "mp3") {
					ext = ".mp3"
				} else if strings.Contains(mime, "ogg") {
					ext = ".ogg"
				}
				attachments = append(attachments, core.Attachment{
					Filename:    "voicemail" + ext,
					ContentType: mime,
					Data:        data,
				})
			}
		}

		inbound := core.InboundMessage{
			ExternalMessageID: fmt.Sprintf("voice:%s", callID),
			SenderPhone:       number,
			SenderName:        req.CallerName,
			Subject:           subject,
			BodyText:          body,
			Attachments:       attachments,
		}

		if err := h.Ingestor.IngestInbound(r.Context(), inbound); err != nil {
			h.Logger.Error("ingest voicemail failed", "caller_number", number, "error", err)
			writeError(w, http.StatusInternalServerError, "voicemail ingestion failed")
			return
		}

		writeJSON(w, http.StatusAccepted, map[string]any{"status": "ingested", "call_id": callID})
		return
	}

	// Path 1: Live incoming ringing call (screen-pop resolution + SSE broadcast)
	resolution, err := h.Contacts.ResolveCaller(r.Context(), core.ChannelPhone, number)
	if err != nil {
		h.Logger.Error("resolve caller failed", "caller_number", number, "error", err)
		writeError(w, http.StatusInternalServerError, "caller lookup failed")
		return
	}

	event := stream.IncomingCallEvent{CallID: req.CallID, CallerNumber: number}
	if resolution != nil {
		event.CallerIDResolution = *resolution
	}
	h.Hub.NotifyIncomingCall(event)

	writeJSON(w, http.StatusAccepted, map[string]any{"status": "ringing", "call_id": req.CallID})
}
