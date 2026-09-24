package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/project-chassis/chassis/internal/core"
)

var _ core.ContactService = (*Repository)(nil)

// GetContact retrieves an individual contact with channels and affiliations.
func (r *Repository) GetContact(ctx context.Context, id int64) (*core.Contact, error) {
	query := `
		SELECT id, full_name, COALESCE(notes, ''), COALESCE(metadata_json, ''), created_at, updated_at
		FROM contacts
		WHERE id = ?;
	`
	var c core.Contact
	var createdAtStr, updatedAtStr string
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.FullName, &c.Notes, &c.MetadataJSON, &createdAtStr, &updatedAtStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, core.ErrContactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query contact %d: %w", id, err)
	}
	c.CreatedAt = parseTime(createdAtStr)
	c.UpdatedAt = parseTime(updatedAtStr)

	// Fetch communication channels
	channels, err := r.getChannels(ctx, &c.ID, nil)
	if err != nil {
		return nil, err
	}
	c.Channels = channels

	// Fetch affiliations
	affiliations, err := r.getContactAffiliations(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	c.Affiliations = affiliations

	return &c, nil
}

// ListContacts returns contacts matching an optional search query.
func (r *Repository) ListContacts(ctx context.Context, query string, limit, offset int) ([]core.Contact, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset = max(0, offset)

	var whereClause string
	var args []any
	if strings.TrimSpace(query) != "" {
		like := "%" + strings.TrimSpace(query) + "%"
		whereClause = `
			WHERE c.full_name LIKE ? OR c.notes LIKE ? 
			   OR EXISTS (SELECT 1 FROM communication_channels cc WHERE cc.contact_id = c.id AND cc.value LIKE ?)
		`
		args = append(args, like, like, like)
	}

	sqlQuery := fmt.Sprintf(`
		SELECT c.id, c.full_name, COALESCE(c.notes, ''), COALESCE(c.metadata_json, ''), c.created_at, c.updated_at
		FROM contacts c
		%s
		ORDER BY c.full_name ASC
		LIMIT ? OFFSET ?;
	`, whereClause)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list contacts: %w", err)
	}
	defer rows.Close()

	var contacts []core.Contact
	for rows.Next() {
		var c core.Contact
		var createdAtStr, updatedAtStr string
		if err := rows.Scan(&c.ID, &c.FullName, &c.Notes, &c.MetadataJSON, &createdAtStr, &updatedAtStr); err != nil {
			return nil, fmt.Errorf("failed to scan contact: %w", err)
		}
		c.CreatedAt = parseTime(createdAtStr)
		c.UpdatedAt = parseTime(updatedAtStr)
		contacts = append(contacts, c)
	}

	// Enrich with primary channels
	for i := range contacts {
		chans, _ := r.getChannels(ctx, &contacts[i].ID, nil)
		contacts[i].Channels = chans
		affils, _ := r.getContactAffiliations(ctx, contacts[i].ID)
		contacts[i].Affiliations = affils
	}

	return contacts, rows.Err()
}

// CreateContact creates a new contact record and any initial channels.
func (r *Repository) CreateContact(ctx context.Context, contact *core.Contact) (*core.Contact, error) {
	if strings.TrimSpace(contact.FullName) == "" {
		return nil, errors.New("contact full_name is required")
	}

	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		query := `
			INSERT INTO contacts (full_name, notes, metadata_json)
			VALUES (?, ?, ?);
		`
		res, err := tx.ExecContext(ctx, query, contact.FullName, contact.Notes, contact.MetadataJSON)
		if err != nil {
			return fmt.Errorf("failed to insert contact: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		contact.ID = id

		for i := range contact.Channels {
			ch := &contact.Channels[i]
			ch.ContactID = &id
			chVal := ch.Value
			if ch.ChannelType == core.ChannelPhone {
				chVal = core.NormalizePhone(chVal)
			} else if ch.ChannelType == core.ChannelEmail {
				chVal = core.NormalizeEmail(chVal)
			}
			isPrimaryInt := 0
			if ch.IsPrimary {
				isPrimaryInt = 1
			}

			chQuery := `
				INSERT INTO communication_channels (contact_id, channel_type, value, label, is_primary)
				VALUES (?, ?, ?, ?, ?);
			`
			chRes, err := tx.ExecContext(ctx, chQuery, id, ch.ChannelType, chVal, ch.Label, isPrimaryInt)
			if err != nil {
				return fmt.Errorf("failed to insert channel %s: %w", chVal, err)
			}
			chID, _ := chRes.LastInsertId()
			ch.ID = chID
			ch.Value = chVal
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return contact, nil
}

// UpdateContact updates an existing contact record.
func (r *Repository) UpdateContact(ctx context.Context, contact *core.Contact) (*core.Contact, error) {
	if contact.ID <= 0 {
		return nil, errors.New("valid contact id is required")
	}
	if strings.TrimSpace(contact.FullName) == "" {
		return nil, errors.New("contact full_name is required")
	}

	query := `
		UPDATE contacts 
		SET full_name = ?, notes = ?, metadata_json = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?;
	`
	res, err := r.db.ExecContext(ctx, query, contact.FullName, contact.Notes, contact.MetadataJSON, contact.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update contact: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return nil, core.ErrContactNotFound
	}
	return r.GetContact(ctx, contact.ID)
}

// DeleteContact removes a contact (channels and affiliations cascade).
func (r *Repository) DeleteContact(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM contacts WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete contact: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return core.ErrContactNotFound
	}
	return nil
}

// GetOrganization retrieves an organization with channels and affiliated contacts.
func (r *Repository) GetOrganization(ctx context.Context, id int64) (*core.Organization, error) {
	query := `
		SELECT id, name, organization_type, COALESCE(notes, ''), COALESCE(metadata_json, ''), created_at, updated_at
		FROM organizations
		WHERE id = ?;
	`
	var o core.Organization
	var createdAtStr, updatedAtStr string
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&o.ID, &o.Name, &o.OrganizationType, &o.Notes, &o.MetadataJSON, &createdAtStr, &updatedAtStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, core.ErrOrganizationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query organization %d: %w", id, err)
	}
	o.CreatedAt = parseTime(createdAtStr)
	o.UpdatedAt = parseTime(updatedAtStr)

	// Fetch communication channels
	channels, err := r.getChannels(ctx, nil, &o.ID)
	if err != nil {
		return nil, err
	}
	o.Channels = channels

	// Fetch affiliated contacts
	contacts, err := r.getOrgAffiliatedContacts(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	o.Contacts = contacts

	return &o, nil
}

// ListOrganizations returns organizations matching an optional search query.
func (r *Repository) ListOrganizations(ctx context.Context, query string, limit, offset int) ([]core.Organization, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset = max(0, offset)

	var whereClause string
	var args []any
	if strings.TrimSpace(query) != "" {
		like := "%" + strings.TrimSpace(query) + "%"
		whereClause = `
			WHERE o.name LIKE ? OR o.notes LIKE ? 
			   OR EXISTS (SELECT 1 FROM communication_channels cc WHERE cc.organization_id = o.id AND cc.value LIKE ?)
		`
		args = append(args, like, like, like)
	}

	sqlQuery := fmt.Sprintf(`
		SELECT o.id, o.name, o.organization_type, COALESCE(o.notes, ''), COALESCE(o.metadata_json, ''), o.created_at, o.updated_at
		FROM organizations o
		%s
		ORDER BY o.name ASC
		LIMIT ? OFFSET ?;
	`, whereClause)
	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list organizations: %w", err)
	}
	defer rows.Close()

	var orgs []core.Organization
	for rows.Next() {
		var o core.Organization
		var createdAtStr, updatedAtStr string
		if err := rows.Scan(&o.ID, &o.Name, &o.OrganizationType, &o.Notes, &o.MetadataJSON, &createdAtStr, &updatedAtStr); err != nil {
			return nil, fmt.Errorf("failed to scan organization: %w", err)
		}
		o.CreatedAt = parseTime(createdAtStr)
		o.UpdatedAt = parseTime(updatedAtStr)
		orgs = append(orgs, o)
	}

	for i := range orgs {
		chans, _ := r.getChannels(ctx, nil, &orgs[i].ID)
		orgs[i].Channels = chans
		affils, _ := r.getOrgAffiliatedContacts(ctx, orgs[i].ID)
		orgs[i].Contacts = affils
	}

	return orgs, rows.Err()
}

// CreateOrganization creates a new organization record.
func (r *Repository) CreateOrganization(ctx context.Context, org *core.Organization) (*core.Organization, error) {
	if strings.TrimSpace(org.Name) == "" {
		return nil, errors.New("organization name is required")
	}
	if org.OrganizationType == "" {
		org.OrganizationType = "DEFAULT"
	}

	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		query := `
			INSERT INTO organizations (name, organization_type, notes, metadata_json)
			VALUES (?, ?, ?, ?);
		`
		res, err := tx.ExecContext(ctx, query, org.Name, org.OrganizationType, org.Notes, org.MetadataJSON)
		if err != nil {
			return fmt.Errorf("failed to insert organization: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		org.ID = id

		for i := range org.Channels {
			ch := &org.Channels[i]
			ch.OrganizationID = &id
			chVal := ch.Value
			if ch.ChannelType == core.ChannelPhone {
				chVal = core.NormalizePhone(chVal)
			} else if ch.ChannelType == core.ChannelEmail {
				chVal = core.NormalizeEmail(chVal)
			}
			isPrimaryInt := 0
			if ch.IsPrimary {
				isPrimaryInt = 1
			}

			chQuery := `
				INSERT INTO communication_channels (organization_id, channel_type, value, label, is_primary)
				VALUES (?, ?, ?, ?, ?);
			`
			chRes, err := tx.ExecContext(ctx, chQuery, id, ch.ChannelType, chVal, ch.Label, isPrimaryInt)
			if err != nil {
				return fmt.Errorf("failed to insert channel %s: %w", chVal, err)
			}
			chID, _ := chRes.LastInsertId()
			ch.ID = chID
			ch.Value = chVal
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return org, nil
}

// UpdateOrganization updates an existing organization.
func (r *Repository) UpdateOrganization(ctx context.Context, org *core.Organization) (*core.Organization, error) {
	if org.ID <= 0 {
		return nil, errors.New("valid organization id is required")
	}
	if strings.TrimSpace(org.Name) == "" {
		return nil, errors.New("organization name is required")
	}

	query := `
		UPDATE organizations
		SET name = ?, organization_type = ?, notes = ?, metadata_json = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?;
	`
	res, err := r.db.ExecContext(ctx, query, org.Name, org.OrganizationType, org.Notes, org.MetadataJSON, org.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update organization: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return nil, core.ErrOrganizationNotFound
	}
	return r.GetOrganization(ctx, org.ID)
}

// DeleteOrganization removes an organization.
func (r *Repository) DeleteOrganization(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM organizations WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete organization: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return core.ErrOrganizationNotFound
	}
	return nil
}

// AddAffiliation establishes or updates an M:N link between a Contact and an Organization.
func (r *Repository) AddAffiliation(ctx context.Context, contactID, orgID int64, roleTitle string, isPrimary bool) error {
	return r.db.WithTx(ctx, func(tx *sql.Tx) error {
		if isPrimary {
			// Unset previous primary affiliation for this contact
			if _, err := tx.ExecContext(ctx, "UPDATE affiliations SET is_primary = 0 WHERE contact_id = ?", contactID); err != nil {
				return err
			}
		}

		isPrimaryInt := 0
		if isPrimary {
			isPrimaryInt = 1
		}

		query := `
			INSERT INTO affiliations (contact_id, organization_id, role_title, is_primary)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(contact_id, organization_id) DO UPDATE SET
				role_title = excluded.role_title,
				is_primary = excluded.is_primary;
		`
		_, err := tx.ExecContext(ctx, query, contactID, orgID, roleTitle, isPrimaryInt)
		return err
	})
}

// RemoveAffiliation severs the relationship between a Contact and an Organization.
func (r *Repository) RemoveAffiliation(ctx context.Context, contactID, orgID int64) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM affiliations WHERE contact_id = ? AND organization_id = ?", contactID, orgID)
	return err
}

// AddChannel adds an email or phone channel to a contact or organization.
func (r *Repository) AddChannel(ctx context.Context, channel *core.CommunicationChannel) (*core.CommunicationChannel, error) {
	if (channel.ContactID == nil && channel.OrganizationID == nil) ||
		(channel.ContactID != nil && channel.OrganizationID != nil) {
		return nil, core.ErrInvalidChannel
	}

	val := channel.Value
	if channel.ChannelType == core.ChannelPhone {
		val = core.NormalizePhone(val)
	} else if channel.ChannelType == core.ChannelEmail {
		val = core.NormalizeEmail(val)
	}
	if val == "" {
		return nil, errors.New("channel value cannot be empty")
	}

	err := r.db.WithTx(ctx, func(tx *sql.Tx) error {
		if channel.IsPrimary {
			if channel.ContactID != nil {
				_, _ = tx.ExecContext(ctx, "UPDATE communication_channels SET is_primary = 0 WHERE contact_id = ? AND channel_type = ?", *channel.ContactID, channel.ChannelType)
			} else if channel.OrganizationID != nil {
				_, _ = tx.ExecContext(ctx, "UPDATE communication_channels SET is_primary = 0 WHERE organization_id = ? AND channel_type = ?", *channel.OrganizationID, channel.ChannelType)
			}
		}

		isPrimaryInt := 0
		if channel.IsPrimary {
			isPrimaryInt = 1
		}

		query := `
			INSERT INTO communication_channels (contact_id, organization_id, channel_type, value, label, is_primary)
			VALUES (?, ?, ?, ?, ?, ?);
		`
		res, err := tx.ExecContext(ctx, query, channel.ContactID, channel.OrganizationID, channel.ChannelType, val, channel.Label, isPrimaryInt)
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return core.ErrChannelConflict
			}
			return fmt.Errorf("failed to add channel: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		channel.ID = id
		channel.Value = val
		return nil
	})

	if err != nil {
		return nil, err
	}
	return channel, nil
}

// RemoveChannel deletes a communication channel.
func (r *Repository) RemoveChannel(ctx context.Context, channelID int64) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM communication_channels WHERE id = ?", channelID)
	return err
}

// ResolveCaller provides fast O(1) indexed identity resolution when a call or email arrives off the wire.
func (r *Repository) ResolveCaller(ctx context.Context, channelType core.ChannelType, value string) (*core.CallerIDResolution, error) {
	val := value
	if channelType == core.ChannelPhone {
		val = core.NormalizePhone(val)
	} else if channelType == core.ChannelEmail {
		val = core.NormalizeEmail(val)
	}
	if val == "" {
		return nil, nil
	}

	query := `
		SELECT id, contact_id, organization_id, channel_type, value, COALESCE(label, ''), is_primary, created_at
		FROM communication_channels
		WHERE channel_type = ? AND value = ?;
	`
	var ch core.CommunicationChannel
	var contactIDVal, orgIDVal sql.NullInt64
	var isPrimaryInt int
	var createdAtStr string

	err := r.db.QueryRowContext(ctx, query, channelType, val).Scan(
		&ch.ID, &contactIDVal, &orgIDVal, &ch.ChannelType, &ch.Value, &ch.Label, &isPrimaryInt, &createdAtStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // Unrecognized caller
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve caller: %w", err)
	}
	ch.IsPrimary = (isPrimaryInt == 1)
	ch.CreatedAt = parseTime(createdAtStr)

	res := &core.CallerIDResolution{
		Channel: ch,
	}

	if contactIDVal.Valid {
		cID := contactIDVal.Int64
		ch.ContactID = &cID
		contact, err := r.GetContact(ctx, cID)
		if err == nil {
			res.Contact = contact
			for _, aff := range contact.Affiliations {
				if aff.IsPrimary {
					affCopy := aff
					res.PrimaryAffiliation = &affCopy
					break
				}
			}
			// If no primary marked but affiliations exist, pick the first
			if res.PrimaryAffiliation == nil && len(contact.Affiliations) > 0 {
				affCopy := contact.Affiliations[0]
				res.PrimaryAffiliation = &affCopy
			}
		}

		// Fetch active work items for contact
		activeItems, err := r.ListQueue(ctx, core.QueueFilter{
			ContactID: &cID,
			Limit:     5,
		})
		if err == nil {
			var openItems []core.WorkItemSummary
			for _, it := range activeItems {
				if it.Status != core.StatusClosed && it.Status != core.StatusResolved {
					openItems = append(openItems, it)
				}
			}
			res.ActiveWorkItems = openItems
		}
	} else if orgIDVal.Valid {
		oID := orgIDVal.Int64
		ch.OrganizationID = &oID
		org, err := r.GetOrganization(ctx, oID)
		if err == nil {
			res.Organization = org
			res.AffiliatedContacts = org.Contacts
		}

		// Fetch active work items for organization
		activeItems, err := r.ListQueue(ctx, core.QueueFilter{
			OrganizationID: &oID,
			Limit:          5,
		})
		if err == nil {
			var openItems []core.WorkItemSummary
			for _, it := range activeItems {
				if it.Status != core.StatusClosed && it.Status != core.StatusResolved {
					openItems = append(openItems, it)
				}
			}
			res.ActiveWorkItems = openItems
		}
	}

	return res, nil
}

// PromoteRequesterToContact promotes an anonymous ticket requester to a persistent contact with optional organization affiliation.
func (r *Repository) PromoteRequesterToContact(ctx context.Context, workItemID int64, organizationName *string, roleTitle *string) (*core.Contact, error) {
	var item core.WorkItem
	var reqName, reqEmail string
	var existingContactID, existingOrgID sql.NullInt64

	err := r.db.QueryRowContext(ctx, `
		SELECT requester_name, requester_email, contact_id, organization_id
		FROM work_items WHERE id = ?;
	`, workItemID).Scan(&reqName, &reqEmail, &existingContactID, &existingOrgID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, core.ErrItemNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query work item: %w", err)
	}

	if reqEmail == "" {
		return nil, errors.New("cannot promote requester: missing requester email on work item")
	}

	normEmail := core.NormalizeEmail(reqEmail)
	if reqName == "" {
		reqName = strings.Split(normEmail, "@")[0]
	}

	var contactID int64
	var orgID *int64

	err = r.db.WithTx(ctx, func(tx *sql.Tx) error {
		// 1. Check if a contact already owns this email channel
		var foundContactID sql.NullInt64
		_ = tx.QueryRowContext(ctx, `
			SELECT contact_id FROM communication_channels
			WHERE channel_type = 'EMAIL' AND value = ?;
		`, normEmail).Scan(&foundContactID)

		if foundContactID.Valid {
			contactID = foundContactID.Int64
		} else {
			// Create new contact
			res, err := tx.ExecContext(ctx, `
				INSERT INTO contacts (full_name) VALUES (?);
			`, reqName)
			if err != nil {
				return fmt.Errorf("failed to create contact: %w", err)
			}
			contactID, err = res.LastInsertId()
			if err != nil {
				return err
			}

			// Add email channel
			_, err = tx.ExecContext(ctx, `
				INSERT INTO communication_channels (contact_id, channel_type, value, label, is_primary)
				VALUES (?, 'EMAIL', ?, 'main', 1);
			`, contactID, normEmail)
			if err != nil {
				return fmt.Errorf("failed to attach email channel: %w", err)
			}
		}

		// 2. Handle optional organization affiliation
		if organizationName != nil && strings.TrimSpace(*organizationName) != "" {
			trimmedOrg := strings.TrimSpace(*organizationName)
			var foundOrgID int64
			err := tx.QueryRowContext(ctx, `
				SELECT id FROM organizations WHERE name = ? COLLATE NOCASE;
			`, trimmedOrg).Scan(&foundOrgID)

			if errors.Is(err, sql.ErrNoRows) {
				orgRes, err := tx.ExecContext(ctx, `
					INSERT INTO organizations (name) VALUES (?);
				`, trimmedOrg)
				if err != nil {
					return fmt.Errorf("failed to create organization %s: %w", trimmedOrg, err)
				}
				foundOrgID, err = orgRes.LastInsertId()
				if err != nil {
					return err
				}
			} else if err != nil {
				return fmt.Errorf("failed to query organization: %w", err)
			}

			orgID = &foundOrgID

			// Add affiliation
			role := "Member"
			if roleTitle != nil && strings.TrimSpace(*roleTitle) != "" {
				role = strings.TrimSpace(*roleTitle)
			}

			// Unset previous primary affiliations for this contact
			_, _ = tx.ExecContext(ctx, "UPDATE affiliations SET is_primary = 0 WHERE contact_id = ?", contactID)

			_, err = tx.ExecContext(ctx, `
				INSERT INTO affiliations (contact_id, organization_id, role_title, is_primary)
				VALUES (?, ?, ?, 1)
				ON CONFLICT(contact_id, organization_id) DO UPDATE SET
					role_title = excluded.role_title,
					is_primary = 1;
			`, contactID, foundOrgID, role)
			if err != nil {
				return fmt.Errorf("failed to link affiliation: %w", err)
			}
		}

		// 3. Link contact_id (and optional organization_id) to the work item
		if orgID != nil {
			_, err = tx.ExecContext(ctx, `
				UPDATE work_items SET contact_id = ?, organization_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;
			`, contactID, *orgID, workItemID)
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE work_items SET contact_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;
			`, contactID, workItemID)
		}
		if err != nil {
			return fmt.Errorf("failed to link work item to contact: %w", err)
		}

		// 4. Audit log entry
		auditDetails := fmt.Sprintf(`{"contact_id":%d,"email":"%s"}`, contactID, normEmail)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_logs (work_item_id, action, details_json)
			VALUES (?, 'REQUESTER_PROMOTED', ?);
		`, workItemID, auditDetails)
		return err
	})

	if err != nil {
		return nil, err
	}

	_ = item // unused variable silence
	return r.GetContact(ctx, contactID)
}

func (r *Repository) getChannels(ctx context.Context, contactID, orgID *int64) ([]core.CommunicationChannel, error) {
	var query string
	var arg any
	if contactID != nil {
		query = `
			SELECT id, contact_id, organization_id, channel_type, value, COALESCE(label, ''), is_primary, created_at
			FROM communication_channels
			WHERE contact_id = ?
			ORDER BY is_primary DESC, id ASC;
		`
		arg = *contactID
	} else if orgID != nil {
		query = `
			SELECT id, contact_id, organization_id, channel_type, value, COALESCE(label, ''), is_primary, created_at
			FROM communication_channels
			WHERE organization_id = ?
			ORDER BY is_primary DESC, id ASC;
		`
		arg = *orgID
	} else {
		return nil, nil
	}

	rows, err := r.db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("failed to query channels: %w", err)
	}
	defer rows.Close()

	var channels []core.CommunicationChannel
	for rows.Next() {
		var ch core.CommunicationChannel
		var cID, oID sql.NullInt64
		var isPrimaryInt int
		var createdAtStr string
		if err := rows.Scan(&ch.ID, &cID, &oID, &ch.ChannelType, &ch.Value, &ch.Label, &isPrimaryInt, &createdAtStr); err != nil {
			return nil, fmt.Errorf("failed to scan channel: %w", err)
		}
		if cID.Valid {
			ch.ContactID = &cID.Int64
		}
		if oID.Valid {
			ch.OrganizationID = &oID.Int64
		}
		ch.IsPrimary = (isPrimaryInt == 1)
		ch.CreatedAt = parseTime(createdAtStr)
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

func (r *Repository) getContactAffiliations(ctx context.Context, contactID int64) ([]core.AffiliationDetail, error) {
	query := `
		SELECT a.id, a.organization_id, o.name, o.organization_type, COALESCE(a.role_title, ''), a.is_primary
		FROM affiliations a
		INNER JOIN organizations o ON a.organization_id = o.id
		WHERE a.contact_id = ?
		ORDER BY a.is_primary DESC, o.name ASC;
	`
	rows, err := r.db.QueryContext(ctx, query, contactID)
	if err != nil {
		return nil, fmt.Errorf("failed to query affiliations: %w", err)
	}
	defer rows.Close()

	var results []core.AffiliationDetail
	for rows.Next() {
		var ad core.AffiliationDetail
		var isPrimaryInt int
		if err := rows.Scan(&ad.AffiliationID, &ad.OrganizationID, &ad.OrganizationName, &ad.OrganizationType, &ad.RoleTitle, &isPrimaryInt); err != nil {
			return nil, fmt.Errorf("failed to scan affiliation: %w", err)
		}
		ad.IsPrimary = (isPrimaryInt == 1)
		results = append(results, ad)
	}
	return results, rows.Err()
}

func (r *Repository) getOrgAffiliatedContacts(ctx context.Context, orgID int64) ([]core.AffiliatedContact, error) {
	query := `
		SELECT a.id, a.contact_id, c.full_name, COALESCE(a.role_title, ''), a.is_primary
		FROM affiliations a
		INNER JOIN contacts c ON a.contact_id = c.id
		WHERE a.organization_id = ?
		ORDER BY a.is_primary DESC, c.full_name ASC;
	`
	rows, err := r.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to query org contacts: %w", err)
	}
	defer rows.Close()

	var results []core.AffiliatedContact
	for rows.Next() {
		var ac core.AffiliatedContact
		var isPrimaryInt int
		if err := rows.Scan(&ac.AffiliationID, &ac.ContactID, &ac.FullName, &ac.RoleTitle, &isPrimaryInt); err != nil {
			return nil, fmt.Errorf("failed to scan affiliated contact: %w", err)
		}
		ac.IsPrimary = (isPrimaryInt == 1)
		results = append(results, ac)
	}
	return results, rows.Err()
}
