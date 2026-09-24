package db

import (
	"context"
	"testing"

	"github.com/project-chassis/chassis/internal/core"
)

func TestContactAndOrganizationLifecycle(t *testing.T) {
	database, repo := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	// 1. Create Organization
	org := &core.Organization{
		Name:             "Acme Foundry & Machine",
		OrganizationType: "VENDOR",
		Notes:            "Key industrial equipment vendor",
		Channels: []core.CommunicationChannel{
			{
				ChannelType: core.ChannelPhone,
				Value:       "(814) 555-0100", // Will be normalized to +18145550100
				Label:       "switchboard",
				IsPrimary:   true,
			},
			{
				ChannelType: core.ChannelEmail,
				Value:       "Dispatch@AcmeFoundry.com", // Will be normalized to dispatch@acmefoundry.com
				Label:       "main",
				IsPrimary:   true,
			},
		},
	}

	createdOrg, err := repo.CreateOrganization(ctx, org)
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}
	if createdOrg.ID == 0 {
		t.Fatal("expected non-zero organization ID")
	}

	// Verify channel normalization
	if len(createdOrg.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(createdOrg.Channels))
	}
	if createdOrg.Channels[0].Value != "+18145550100" {
		t.Errorf("expected normalized phone +18145550100, got %s", createdOrg.Channels[0].Value)
	}
	if createdOrg.Channels[1].Value != "dispatch@acmefoundry.com" {
		t.Errorf("expected normalized email dispatch@acmefoundry.com, got %s", createdOrg.Channels[1].Value)
	}

	// 2. Create Contact
	contact := &core.Contact{
		FullName: "Frank Miller",
		Notes:    "Plant superintendent",
		Channels: []core.CommunicationChannel{
			{
				ChannelType: core.ChannelPhone,
				Value:       "814.555.0142",
				Label:       "mobile",
				IsPrimary:   true,
			},
			{
				ChannelType: core.ChannelEmail,
				Value:       "fmiller@acmefoundry.com",
				Label:       "direct",
				IsPrimary:   true,
			},
		},
	}

	createdContact, err := repo.CreateContact(ctx, contact)
	if err != nil {
		t.Fatalf("CreateContact failed: %v", err)
	}
	if createdContact.ID == 0 {
		t.Fatal("expected non-zero contact ID")
	}
	if createdContact.Channels[0].Value != "+18145550142" {
		t.Errorf("expected normalized phone +18145550142, got %s", createdContact.Channels[0].Value)
	}

	// 3. Link Affiliation
	err = repo.AddAffiliation(ctx, createdContact.ID, createdOrg.ID, "Plant Superintendent", true)
	if err != nil {
		t.Fatalf("AddAffiliation failed: %v", err)
	}

	// Re-fetch contact and verify affiliation
	fetchedContact, err := repo.GetContact(ctx, createdContact.ID)
	if err != nil {
		t.Fatalf("GetContact failed: %v", err)
	}
	if len(fetchedContact.Affiliations) != 1 {
		t.Fatalf("expected 1 affiliation, got %d", len(fetchedContact.Affiliations))
	}
	if fetchedContact.Affiliations[0].OrganizationName != "Acme Foundry & Machine" {
		t.Errorf("expected org name Acme Foundry & Machine, got %s", fetchedContact.Affiliations[0].OrganizationName)
	}
	if fetchedContact.Affiliations[0].RoleTitle != "Plant Superintendent" {
		t.Errorf("expected role title Plant Superintendent, got %s", fetchedContact.Affiliations[0].RoleTitle)
	}

	// Re-fetch organization and verify contact listed
	fetchedOrg, err := repo.GetOrganization(ctx, createdOrg.ID)
	if err != nil {
		t.Fatalf("GetOrganization failed: %v", err)
	}
	if len(fetchedOrg.Contacts) != 1 {
		t.Fatalf("expected 1 affiliated contact, got %d", len(fetchedOrg.Contacts))
	}
	if fetchedOrg.Contacts[0].FullName != "Frank Miller" {
		t.Errorf("expected Frank Miller, got %s", fetchedOrg.Contacts[0].FullName)
	}

	// 4. Test Partial Unique Index (one primary channel per type)
	// Adding a second primary phone channel for contact should update/enforce cleanly
	secondPhone := &core.CommunicationChannel{
		ContactID:   &createdContact.ID,
		ChannelType: core.ChannelPhone,
		Value:       "814-555-0999",
		Label:       "desk",
		IsPrimary:   true,
	}
	addedPhone, err := repo.AddChannel(ctx, secondPhone)
	if err != nil {
		t.Fatalf("AddChannel failed: %v", err)
	}
	if !addedPhone.IsPrimary {
		t.Error("expected second phone to be primary")
	}

	refetchedContact, err := repo.GetContact(ctx, createdContact.ID)
	if err != nil {
		t.Fatalf("GetContact failed: %v", err)
	}
	var primaryPhoneCount int
	for _, ch := range refetchedContact.Channels {
		if ch.ChannelType == core.ChannelPhone && ch.IsPrimary {
			primaryPhoneCount++
		}
	}
	if primaryPhoneCount != 1 {
		t.Errorf("expected exactly 1 primary phone, got %d", primaryPhoneCount)
	}
}

func TestResolveCallerAndPromote(t *testing.T) {
	database, repo := setupTestDB(t)
	defer database.Close()
	ctx := context.Background()

	// 1. Create a work item
	item := &core.WorkItem{
		ItemCode:       "HD-2001",
		DomainType:     core.DomainIT,
		RequesterName:  "Sarah Connor",
		RequesterEmail: "sconnor@cyberdyne.org",
		Subject:        "Server room AC failure",
		Summary:        "Chiller unit warning light blinking",
		Priority:       core.PriorityHigh,
	}
	createdItem, err := repo.CreateItem(ctx, item, nil)
	if err != nil {
		t.Fatalf("CreateItem failed: %v", err)
	}

	// 2. Promote Requester to Contact with Organization Affiliation
	orgName := "Cyberdyne Systems"
	role := "Security Director"
	promoted, err := repo.PromoteRequesterToContact(ctx, createdItem.ID, &orgName, &role)
	if err != nil {
		t.Fatalf("PromoteRequesterToContact failed: %v", err)
	}
	if promoted.FullName != "Sarah Connor" {
		t.Errorf("expected Sarah Connor, got %s", promoted.FullName)
	}
	if len(promoted.Channels) != 1 || promoted.Channels[0].Value != "sconnor@cyberdyne.org" {
		t.Errorf("expected email channel sconnor@cyberdyne.org, got %+v", promoted.Channels)
	}
	if len(promoted.Affiliations) != 1 || promoted.Affiliations[0].OrganizationName != "Cyberdyne Systems" {
		t.Errorf("expected affiliation to Cyberdyne Systems, got %+v", promoted.Affiliations)
	}

	// Verify the work item now has contact_id and organization_id linked
	updatedDetail, err := repo.GetItem(ctx, createdItem.ID)
	if err != nil {
		t.Fatalf("GetItem failed: %v", err)
	}
	if updatedDetail.Item.ContactID == nil || *updatedDetail.Item.ContactID != promoted.ID {
		t.Errorf("expected work item contact_id = %d, got %v", promoted.ID, updatedDetail.Item.ContactID)
	}
	if updatedDetail.Item.OrganizationID == nil {
		t.Error("expected work item organization_id to be populated")
	}

	// 3. Add mobile phone to Sarah Connor
	phoneChan := &core.CommunicationChannel{
		ContactID:   &promoted.ID,
		ChannelType: core.ChannelPhone,
		Value:       "(814) 555-4000",
		Label:       "mobile",
		IsPrimary:   true,
	}
	if _, err := repo.AddChannel(ctx, phoneChan); err != nil {
		t.Fatalf("AddChannel failed: %v", err)
	}

	// 4. Test ResolveCaller - Path B (Direct line to Sarah Connor)
	res, err := repo.ResolveCaller(ctx, core.ChannelPhone, "814.555.4000")
	if err != nil {
		t.Fatalf("ResolveCaller failed: %v", err)
	}
	if res == nil || res.Contact == nil {
		t.Fatal("expected caller resolution for Sarah Connor, got nil")
	}
	if res.Contact.FullName != "Sarah Connor" {
		t.Errorf("expected Sarah Connor, got %s", res.Contact.FullName)
	}
	if res.PrimaryAffiliation == nil || res.PrimaryAffiliation.OrganizationName != "Cyberdyne Systems" {
		t.Errorf("expected primary affiliation Cyberdyne Systems, got %+v", res.PrimaryAffiliation)
	}
	if len(res.ActiveWorkItems) != 1 || res.ActiveWorkItems[0].ItemCode != "HD-2001" {
		t.Errorf("expected active ticket HD-2001, got %+v", res.ActiveWorkItems)
	}

	// 5. Test ResolveCaller - Path A (Organization Switchboard)
	orgID := *updatedDetail.Item.OrganizationID
	orgSwitchboard := &core.CommunicationChannel{
		OrganizationID: &orgID,
		ChannelType:    core.ChannelPhone,
		Value:          "814-555-1000",
		Label:          "switchboard",
		IsPrimary:      true,
	}
	if _, err := repo.AddChannel(ctx, orgSwitchboard); err != nil {
		t.Fatalf("AddChannel switchboard failed: %v", err)
	}

	orgRes, err := repo.ResolveCaller(ctx, core.ChannelPhone, "+18145551000")
	if err != nil {
		t.Fatalf("ResolveCaller org failed: %v", err)
	}
	if orgRes == nil || orgRes.Organization == nil {
		t.Fatal("expected caller resolution for Cyberdyne Systems, got nil")
	}
	if orgRes.Organization.Name != "Cyberdyne Systems" {
		t.Errorf("expected Cyberdyne Systems, got %s", orgRes.Organization.Name)
	}
	if len(orgRes.AffiliatedContacts) != 1 || orgRes.AffiliatedContacts[0].FullName != "Sarah Connor" {
		t.Errorf("expected affiliated contact Sarah Connor, got %+v", orgRes.AffiliatedContacts)
	}
	if len(orgRes.ActiveWorkItems) != 1 || orgRes.ActiveWorkItems[0].ItemCode != "HD-2001" {
		t.Errorf("expected active ticket HD-2001 on org, got %+v", orgRes.ActiveWorkItems)
	}

	// 6. Test Unrecognized caller
	unknownRes, err := repo.ResolveCaller(ctx, core.ChannelPhone, "+19999999999")
	if err != nil {
		t.Fatalf("ResolveCaller unknown failed: %v", err)
	}
	if unknownRes != nil {
		t.Errorf("expected nil resolution for unknown phone, got %+v", unknownRes)
	}
}
