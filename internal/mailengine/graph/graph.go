package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/project-chassis/chassis/internal/mailengine"
)

var (
	ErrMissingConfig     = errors.New("missing required Microsoft Graph configuration")
	ErrRefuseInternalMsg = errors.New("SECURITY VIOLATION: Refusing to dispatch internal note externally")
)

// Config contains Azure AD / Microsoft 365 App Registration credentials.
type Config struct {
	TenantID     string
	ClientID     string
	ClientSecret string
	MailboxEmail string
}

// Provider implements mailengine.MailProvider for Microsoft 365 / Microsoft Graph.
type Provider struct {
	cfg        Config
	httpClient *http.Client

	mu          sync.RWMutex
	accessToken string
	tokenExpiry time.Time
}

var _ mailengine.MailProvider = (*Provider)(nil)

// NewProvider creates a new Microsoft Graph mail provider.
func NewProvider(cfg Config) *Provider {
	return &Provider{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *Provider) Name() string {
	return "GRAPH"
}

func (p *Provider) AccountID() string {
	return p.cfg.MailboxEmail
}

func (p *Provider) Initialize(ctx context.Context, config map[string]string) error {
	if config != nil {
		if v, ok := config["tenant_id"]; ok {
			p.cfg.TenantID = v
		}
		if v, ok := config["client_id"]; ok {
			p.cfg.ClientID = v
		}
		if v, ok := config["client_secret"]; ok {
			p.cfg.ClientSecret = v
		}
		if v, ok := config["mailbox_email"]; ok {
			p.cfg.MailboxEmail = v
		}
	}

	if p.cfg.TenantID == "" || p.cfg.ClientID == "" || p.cfg.ClientSecret == "" || p.cfg.MailboxEmail == "" {
		return ErrMissingConfig
	}

	// Validate credentials by fetching initial token
	_, err := p.getValidToken(ctx)
	return err
}

// FetchNewMessages retrieves unread messages or uses delta query links from Microsoft Graph.
func (p *Provider) FetchNewMessages(ctx context.Context, deltaToken string) ([]mailengine.InboundMessage, string, error) {
	token, err := p.getValidToken(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("failed to obtain Graph access token: %w", err)
	}

	var reqURL string
	if deltaToken != "" {
		// Delta token contains the full @odata.deltaLink or @odata.nextLink
		reqURL = deltaToken
	} else {
		// Initial query: fetch recent inbox messages with internet message headers
		reqURL = fmt.Sprintf(
			"https://graph.microsoft.com/v1.0/users/%s/mailFolders/inbox/messages/delta?$select=id,internetMessageId,conversationId,subject,bodyPreview,body,from,internetMessageHeaders,hasAttachments",
			url.PathEscape(p.cfg.MailboxEmail),
		)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Prefer", "odata.maxpagesize=50")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("graph request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("graph api error (status %d): %s", resp.StatusCode, string(body))
	}

	var graphResp struct {
		Value []struct {
			ID                     string `json:"id"`
			InternetMessageID      string `json:"internetMessageId"`
			ConversationID         string `json:"conversationId"`
			Subject                string `json:"subject"`
			BodyPreview            string `json:"bodyPreview"`
			Body                   struct {
				ContentType string `json:"contentType"`
				Content     string `json:"content"`
			} `json:"body"`
			From struct {
				EmailAddress struct {
					Name    string `json:"name"`
					Address string `json:"address"`
				} `json:"emailAddress"`
			} `json:"from"`
			InternetMessageHeaders []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"internetMessageHeaders"`
		} `json:"value"`
		NextLink  string `json:"@odata.nextLink"`
		DeltaLink string `json:"@odata.deltaLink"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&graphResp); err != nil {
		return nil, "", fmt.Errorf("failed to decode graph response: %w", err)
	}

	var messages []mailengine.InboundMessage
	for _, item := range graphResp.Value {
		// Ignore messages sent by ourselves
		if strings.EqualFold(item.From.EmailAddress.Address, p.cfg.MailboxEmail) {
			continue
		}

		inbound := mailengine.InboundMessage{
			ExternalMessageID: item.InternetMessageID,
			ThreadID:          item.ConversationID,
			SenderEmail:       item.From.EmailAddress.Address,
			SenderName:        item.From.EmailAddress.Name,
			Subject:           item.Subject,
			BodyText:          item.BodyPreview,
		}

		if strings.EqualFold(item.Body.ContentType, "html") {
			inbound.BodyHTML = item.Body.Content
		} else {
			inbound.BodyText = item.Body.Content
		}

		// Extract In-Reply-To and References from headers
		for _, h := range item.InternetMessageHeaders {
			switch strings.ToLower(h.Name) {
			case "in-reply-to":
				inbound.InReplyTo = h.Value
			case "references":
				inbound.References = strings.Fields(h.Value)
			}
		}

		if inbound.ExternalMessageID == "" {
			inbound.ExternalMessageID = item.ID // fallback to Graph ID if internetMessageId missing
		}

		messages = append(messages, inbound)
	}

	nextCheckpoint := graphResp.DeltaLink
	if graphResp.NextLink != "" {
		nextCheckpoint = graphResp.NextLink
	}

	return messages, nextCheckpoint, nil
}

// SendMessage dispatches an email via Microsoft Graph /sendMail.
// Strictly enforces the Ancestral Mandate: IsInternal messages can NEVER be dispatched.
func (p *Provider) SendMessage(ctx context.Context, msg mailengine.OutboundMessage) error {
	if msg.IsInternal {
		return ErrRefuseInternalMsg
	}
	if strings.TrimSpace(msg.Recipient) == "" {
		return mailengine.ErrEmptyRecipient
	}

	token, err := p.getValidToken(ctx)
	if err != nil {
		return err
	}

	reqURL := fmt.Sprintf("https://graph.microsoft.com/v1.0/users/%s/sendMail", url.PathEscape(p.cfg.MailboxEmail))

	headers := []map[string]string{}
	if msg.InReplyTo != "" {
		headers = append(headers, map[string]string{"name": "In-Reply-To", "value": msg.InReplyTo})
	}
	if msg.References != "" {
		headers = append(headers, map[string]string{"name": "References", "value": msg.References})
	}

	payload := map[string]any{
		"message": map[string]any{
			"subject": msg.Subject,
			"body": map[string]string{
				"contentType": "Text",
				"content":     msg.Body,
			},
			"toRecipients": []map[string]any{
				{
					"emailAddress": map[string]string{
						"address": msg.Recipient,
					},
				},
			},
			"internetMessageHeaders": headers,
		},
		"saveToSentItems": true,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email via graph: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("graph sendMail error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (p *Provider) HealthCheck(ctx context.Context) error {
	_, err := p.getValidToken(ctx)
	return err
}

func (p *Provider) getValidToken(ctx context.Context) (string, error) {
	p.mu.RLock()
	if p.accessToken != "" && time.Now().Before(p.tokenExpiry.Add(-2*time.Minute)) {
		token := p.accessToken
		p.mu.RUnlock()
		return token, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if p.accessToken != "" && time.Now().Before(p.tokenExpiry.Add(-2*time.Minute)) {
		return p.accessToken, nil
	}

	tokenURL := fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", url.PathEscape(p.cfg.TenantID))
	form := url.Values{}
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("scope", "https://graph.microsoft.com/.default")
	form.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth2 token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("oauth2 token failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode oauth2 token response: %w", err)
	}

	p.accessToken = tokenResp.AccessToken
	p.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return p.accessToken, nil
}
