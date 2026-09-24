package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/web/stream"
)

type mockContactService struct {
	resolveFn func(ctx context.Context, channelType core.ChannelType, value string) (*core.CallerIDResolution, error)
}

func (m *mockContactService) GetContact(ctx context.Context, id int64) (*core.Contact, error) {
	return nil, nil
}
func (m *mockContactService) ListContacts(ctx context.Context, query string, limit, offset int) ([]core.Contact, error) {
	return nil, nil
}
func (m *mockContactService) CreateContact(ctx context.Context, contact *core.Contact) (*core.Contact, error) {
	return nil, nil
}
func (m *mockContactService) UpdateContact(ctx context.Context, contact *core.Contact) (*core.Contact, error) {
	return nil, nil
}
func (m *mockContactService) DeleteContact(ctx context.Context, id int64) error {
	return nil
}
func (m *mockContactService) GetOrganization(ctx context.Context, id int64) (*core.Organization, error) {
	return nil, nil
}
func (m *mockContactService) ListOrganizations(ctx context.Context, query string, limit, offset int) ([]core.Organization, error) {
	return nil, nil
}
func (m *mockContactService) CreateOrganization(ctx context.Context, org *core.Organization) (*core.Organization, error) {
	return nil, nil
}
func (m *mockContactService) UpdateOrganization(ctx context.Context, org *core.Organization) (*core.Organization, error) {
	return nil, nil
}
func (m *mockContactService) DeleteOrganization(ctx context.Context, id int64) error {
	return nil
}
func (m *mockContactService) AddAffiliation(ctx context.Context, contactID, orgID int64, roleTitle string, isPrimary bool) error {
	return nil
}
func (m *mockContactService) RemoveAffiliation(ctx context.Context, contactID, orgID int64) error {
	return nil
}
func (m *mockContactService) AddChannel(ctx context.Context, channel *core.CommunicationChannel) (*core.CommunicationChannel, error) {
	return nil, nil
}
func (m *mockContactService) RemoveChannel(ctx context.Context, channelID int64) error {
	return nil
}
func (m *mockContactService) ResolveCaller(ctx context.Context, channelType core.ChannelType, value string) (*core.CallerIDResolution, error) {
	if m.resolveFn != nil {
		return m.resolveFn(ctx, channelType, value)
	}
	return nil, nil
}
func (m *mockContactService) PromoteRequesterToContact(ctx context.Context, workItemID int64, organizationName *string, roleTitle *string) (*core.Contact, error) {
	return nil, nil
}

type mockIngestor struct {
	ingested []core.InboundMessage
}

func (m *mockIngestor) IngestInbound(ctx context.Context, msg core.InboundMessage) error {
	m.ingested = append(m.ingested, msg)
	return nil
}

func TestVoiceIncoming_Auth(t *testing.T) {
	h := &Handlers{VoiceWebhookSecret: "super-secret"}

	body := bytes.NewBufferString(`{"caller_number": "8145550199"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/voice/incoming", body)
	rec := httptest.NewRecorder()

	h.VoiceIncoming(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized without header, got %d", rec.Code)
	}

	req.Header.Set("X-Webhook-Secret", "wrong-secret")
	rec = httptest.NewRecorder()
	h.VoiceIncoming(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized with wrong secret, got %d", rec.Code)
	}
}

func TestVoiceIncoming_LiveCall(t *testing.T) {
	hub := stream.NewHub()
	ch, cancel := hub.SubscribeOperator()
	defer cancel()

	mockContacts := &mockContactService{
		resolveFn: func(ctx context.Context, channelType core.ChannelType, value string) (*core.CallerIDResolution, error) {
			if value == "+18145550199" {
				return &core.CallerIDResolution{
					Contact: &core.Contact{ID: 42, FullName: "Frank Miller"},
					PrimaryAffiliation: &core.AffiliationDetail{
						OrganizationName: "Acme Metal Co.",
						RoleTitle:        "Plant Superintendent",
					},
				}, nil
			}
			return nil, nil
		},
	}

	h := &Handlers{
		Contacts:           mockContacts,
		Hub:                hub,
		VoiceWebhookSecret: "test-secret",
	}

	payload := map[string]any{
		"call_id":       "call-1234",
		"caller_number": "(814) 555-0199",
		"caller_name":   "Frank Miller",
	}
	data, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/voice/incoming", bytes.NewReader(data))
	req.Header.Set("X-Webhook-Secret", "test-secret")
	rec := httptest.NewRecorder()

	h.VoiceIncoming(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify broadcast event on operator SSE hub
	select {
	case ev := <-ch:
		if ev.Name != "incoming_call" {
			t.Fatalf("expected event incoming_call, got %s", ev.Name)
		}
		var callEv stream.IncomingCallEvent
		if err := json.Unmarshal(ev.Data, &callEv); err != nil {
			t.Fatalf("failed to unmarshal call event: %v", err)
		}
		if callEv.CallID != "call-1234" {
			t.Errorf("expected call_id call-1234, got %s", callEv.CallID)
		}
		if callEv.CallerNumber != "+18145550199" {
			t.Errorf("expected normalized caller number +18145550199, got %s", callEv.CallerNumber)
		}
		if callEv.Contact == nil || callEv.Contact.FullName != "Frank Miller" {
			t.Errorf("expected resolved contact Frank Miller, got %+v", callEv.Contact)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for operator SSE incoming_call event")
	}
}

func TestVoiceIncoming_Voicemail(t *testing.T) {
	mockIng := &mockIngestor{}
	h := &Handlers{
		Ingestor:           mockIng,
		VoiceWebhookSecret: "test-secret",
	}

	audioSample := []byte("RIFFwavdata12345")
	b64Audio := base64.StdEncoding.EncodeToString(audioSample)

	payload := map[string]any{
		"event_type":    "voicemail",
		"call_id":       "call-9876",
		"caller_number": "8145550100",
		"caller_name":   "Alice Chen",
		"transcript":    "The boiler pressure is spiking, please dispatch technician immediately.",
		"recording_url": "https://pbx.internal/recordings/call-9876.wav",
		"audio_base64":  b64Audio,
		"audio_mime":    "audio/wav",
	}
	data, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/voice/incoming", bytes.NewReader(data))
	req.Header.Set("X-Webhook-Secret", "test-secret")
	rec := httptest.NewRecorder()

	h.VoiceIncoming(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(mockIng.ingested) != 1 {
		t.Fatalf("expected 1 ingested message, got %d", len(mockIng.ingested))
	}

	msg := mockIng.ingested[0]
	if msg.ExternalMessageID != "voice:call-9876" {
		t.Errorf("expected external ID voice:call-9876, got %s", msg.ExternalMessageID)
	}
	if msg.SenderPhone != "+18145550100" {
		t.Errorf("expected normalized phone +18145550100, got %s", msg.SenderPhone)
	}
	if msg.SenderName != "Alice Chen" {
		t.Errorf("expected sender name Alice Chen, got %s", msg.SenderName)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	if string(msg.Attachments[0].Data) != string(audioSample) {
		t.Errorf("audio attachment data mismatch")
	}
}
