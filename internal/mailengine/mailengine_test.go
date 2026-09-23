package mailengine

import (
	"context"
	"testing"

	"github.com/project-chassis/chassis/internal/core"
	"github.com/project-chassis/chassis/internal/db"
)

type mockProvider struct {
	name      string
	accountID string
	messages  []InboundMessage
	sent      []OutboundMessage
}

func (m *mockProvider) Name() string      { return m.name }
func (m *mockProvider) AccountID() string { return m.accountID }
func (m *mockProvider) Initialize(ctx context.Context, cfg map[string]string) error {
	return nil
}
func (m *mockProvider) FetchNewMessages(ctx context.Context, deltaToken string) ([]InboundMessage, string, error) {
	return m.messages, "delta-token-v2", nil
}
func (m *mockProvider) SendMessage(ctx context.Context, msg OutboundMessage) error {
	m.sent = append(m.sent, msg)
	return nil
}
func (m *mockProvider) HealthCheck(ctx context.Context) error {
	return nil
}

func setupTestRepo(t *testing.T) (*db.DB, *db.Repository) {
	t.Helper()
	cfg := db.Config{Path: ":memory:", BusyTimeout: 5000}
	database, err := db.Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}
	return database, db.NewRepository(database)
}

func TestThreaderCorrelation(t *testing.T) {
	database, repo := setupTestRepo(t)
	defer database.Close()
	ctx := context.Background()

	// 1. Create baseline ticket
	item := &core.WorkItem{
		ItemCode:       "HD-1001",
		DomainType:     core.DomainIT,
		RequesterName:  "Alice",
		RequesterEmail: "alice@example.com",
		Subject:        "Laptop won't boot",
	}
	initialMsg := &core.ThreadMessage{
		SenderEmail:       "alice@example.com",
		Body:              "Help me",
		ExternalMessageID: ptr("msg-id-12345"),
	}
	created, err := repo.CreateItem(ctx, item, initialMsg)
	if err != nil {
		t.Fatalf("CreateItem failed: %v", err)
	}

	threader := NewThreader(database)

	// Test Stage 1: Header match (In-Reply-To matching msg-id-12345)
	res1, err := threader.Correlate(ctx, InboundMessage{
		Subject:     "Re: Broken laptop",
		SenderEmail: "alice@example.com",
		InReplyTo:   "<msg-id-12345>",
	})
	if err != nil {
		t.Fatalf("Correlate failed: %v", err)
	}
	if res1.Stage != "HEADER_MATCH" || res1.WorkItemID != created.ID {
		t.Fatalf("expected HEADER_MATCH on item %d, got stage=%s id=%d", created.ID, res1.Stage, res1.WorkItemID)
	}

	// Test Stage 2: Subject Regex match [HD-1001]
	res2, err := threader.Correlate(ctx, InboundMessage{
		Subject:     "Update on [HD-1001] - still broken",
		SenderEmail: "alice@example.com",
	})
	if err != nil {
		t.Fatalf("Correlate failed: %v", err)
	}
	if res2.Stage != "SUBJECT_REGEX" || res2.WorkItemID != created.ID {
		t.Fatalf("expected SUBJECT_REGEX on item %d, got stage=%s id=%d", created.ID, res2.Stage, res2.WorkItemID)
	}

	// Test Stage 3: New Item (no headers, no code in subject)
	res3, err := threader.Correlate(ctx, InboundMessage{
		Subject:     "Brand new question",
		SenderEmail: "bob@example.com",
	})
	if err != nil {
		t.Fatalf("Correlate failed: %v", err)
	}
	if res3.Stage != "NEW_ITEM" || res3.WorkItemID != 0 {
		t.Fatalf("expected NEW_ITEM, got stage=%s id=%d", res3.Stage, res3.WorkItemID)
	}
}

type mockNotifier struct {
	notifications []struct {
		itemID int64
		msg    core.ThreadMessage
	}
}

func (m *mockNotifier) NotifyNewMessage(workItemID int64, msg core.ThreadMessage) {
	m.notifications = append(m.notifications, struct {
		itemID int64
		msg    core.ThreadMessage
	}{itemID: workItemID, msg: msg})
}

func TestSyncWorkerIngestion(t *testing.T) {
	database, repo := setupTestRepo(t)
	defer database.Close()
	ctx := context.Background()

	mock := &mockProvider{
		name:      "MOCK_GRAPH",
		accountID: "helpdesk@local.org",
		messages: []InboundMessage{
			{
				ExternalMessageID: "msg-ext-001",
				SenderEmail:       "charlie@citizen.org",
				SenderName:        "Charlie",
				Subject:           "New Road Hazard",
				BodyText:          "Pothole on Main St.",
			},
		},
	}

	notifier := &mockNotifier{}

	worker := NewSyncWorker(WorkerConfig{
		Provider:      mock,
		Repository:    repo,
		Notifier:      notifier,
		DefaultPrefix: "CW",
	})

	// Run sync cycle
	if err := worker.SyncOnce(ctx); err != nil {
		t.Fatalf("SyncOnce failed: %v", err)
	}

	// Verify checkpoint was updated
	checkpoint, err := repo.GetCheckpoint(ctx, "MOCK_GRAPH", "helpdesk@local.org")
	if err != nil {
		t.Fatalf("GetCheckpoint failed: %v", err)
	}
	if checkpoint != "delta-token-v2" {
		t.Fatalf("expected checkpoint delta-token-v2, got %s", checkpoint)
	}

	// Verify item was created
	items, err := repo.ListQueue(ctx, core.QueueFilter{})
	if err != nil {
		t.Fatalf("ListQueue failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Subject != "New Road Hazard" {
		t.Fatalf("unexpected subject: %s", items[0].Subject)
	}

	// Verify Notifier was called for live SSE streams
	if len(notifier.notifications) != 1 {
		t.Fatalf("expected exactly 1 notification, got %d", len(notifier.notifications))
	}
	notif := notifier.notifications[0]
	if notif.itemID != items[0].ID {
		t.Fatalf("expected notification item ID %d, got %d", items[0].ID, notif.itemID)
	}
	if notif.msg.Body != "Pothole on Main St." {
		t.Fatalf("unexpected notification body: %s", notif.msg.Body)
	}

	// Test idempotency: re-running sync with same message should not duplicate or notify
	if err := worker.SyncOnce(ctx); err != nil {
		t.Fatalf("Second SyncOnce failed: %v", err)
	}
	itemsAfter, _ := repo.ListQueue(ctx, core.QueueFilter{})
	if len(itemsAfter) != 1 {
		t.Fatalf("expected still 1 item after duplicate sync, got %d", len(itemsAfter))
	}
	if len(notifier.notifications) != 1 {
		t.Fatalf("expected no extra notifications on duplicate sync, got %d", len(notifier.notifications))
	}
}

func ptr[T any](v T) *T {
	return &v
}
