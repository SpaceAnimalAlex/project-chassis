package db

import (
	"context"
	"testing"

	"github.com/project-chassis/chassis/internal/core"
)

func setupTestDB(t *testing.T) (*DB, *Repository) {
	t.Helper()
	cfg := Config{
		Path:        ":memory:",
		BusyTimeout: 5000,
	}
	database, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	ctx := context.Background()
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	repo := NewRepository(database)
	return database, repo
}

func TestMigrationAndRepository(t *testing.T) {
	database, repo := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	// 1. Create a user
	_, err := database.ExecContext(ctx, `
		INSERT INTO users (uuid, email, full_name, role) 
		VALUES ('user-1', 'tech@local.org', 'Lead Tech', 'AGENT');
	`)
	if err != nil {
		t.Fatalf("Failed to insert user: %v", err)
	}

	// 2. Create work item with initial message
	item := &core.WorkItem{
		ItemCode:       "HD-1001",
		DomainType:     core.DomainIT,
		RequesterName:  "Jane Constituent",
		RequesterEmail: "jane@citizen.org",
		Subject:        "Printer offline in room 3",
		Summary:        "Laser printer won't connect to network",
		Priority:       core.PriorityNormal,
	}
	initialMsg := &core.ThreadMessage{
		SenderEmail: "jane@citizen.org",
		Body:        "Please help, printer shows error 50.",
		IsInternal:  false,
	}

	created, err := repo.CreateItem(ctx, item, initialMsg)
	if err != nil {
		t.Fatalf("CreateItem failed: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected positive ID, got %d", created.ID)
	}
	if created.Status != core.StatusNew {
		t.Fatalf("expected status NEW, got %s", created.Status)
	}

	// 3. Assign item to tech -> should succeed
	var techID int64 = 1
	if err := repo.AssignItem(ctx, created.ID, techID, &techID); err != nil {
		t.Fatalf("AssignItem failed: %v", err)
	}

	// 4. Transition: NEW -> OPEN
	if err := repo.TransitionStatus(ctx, created.ID, techID, core.StatusOpen); err != nil {
		t.Fatalf("Transition to OPEN failed: %v", err)
	}

	// 5. Add internal note (is_internal = true)
	note, err := repo.AddThreadMessage(ctx, created.ID, &techID, "tech@local.org", "Checked switch port, resetting DHCP lease.", true, nil)
	if err != nil {
		t.Fatalf("AddThreadMessage (internal) failed: %v", err)
	}
	if !note.IsInternal {
		t.Fatal("expected message to be internal")
	}

	// 6. Add public reply (is_internal = false) -> should automatically transition status to PENDING_USER
	reply, err := repo.AddThreadMessage(ctx, created.ID, &techID, "tech@local.org", "Jane, please power cycle the printer now.", false, nil)
	if err != nil {
		t.Fatalf("AddThreadMessage (public) failed: %v", err)
	}
	if reply.IsInternal {
		t.Fatal("expected message to be public")
	}

	detail, err := repo.GetItem(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetItem failed: %v", err)
	}
	if detail.Item.Status != core.StatusPendingUser {
		t.Fatalf("expected automatic status PENDING_USER after public reply, got %s", detail.Item.Status)
	}
	if len(detail.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(detail.Messages))
	}

	// 7. Requester replies back (authorID == nil) -> should automatically transition back to OPEN
	_, err = repo.AddThreadMessage(ctx, created.ID, nil, "jane@citizen.org", "It works now! Thank you!", false, nil)
	if err != nil {
		t.Fatalf("Requester reply failed: %v", err)
	}

	detailAfterReply, err := repo.GetItem(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetItem after requester reply failed: %v", err)
	}
	if detailAfterReply.Item.Status != core.StatusOpen {
		t.Fatalf("expected automatic status OPEN after requester reply, got %s", detailAfterReply.Item.Status)
	}

	// 8. Close ticket
	if err := repo.TransitionStatus(ctx, created.ID, techID, core.StatusResolved); err != nil {
		t.Fatalf("Transition to RESOLVED failed: %v", err)
	}

	// 9. Verify Audit Trail
	if len(detailAfterReply.AuditLog) < 4 {
		t.Fatalf("expected at least 4 audit records, got %d", len(detailAfterReply.AuditLog))
	}

	// 10. Verify SanitizeForRequester never leaks internal notes
	sanitized := core.SanitizeForRequester(detailAfterReply.Messages)
	for _, m := range sanitized {
		if m.IsInternal {
			t.Fatal("CRITICAL SECURITY VIOLATION: Internal note leaked into sanitized requester thread!")
		}
	}
	if len(sanitized) != 3 { // 1 initial + 1 public reply + 1 requester reply = 3 public
		t.Fatalf("expected exactly 3 public messages, got %d", len(sanitized))
	}
}
