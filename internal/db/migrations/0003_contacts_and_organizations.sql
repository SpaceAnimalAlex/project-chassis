-- 0003_contacts_and_organizations.sql: Organizations, Contacts, Affiliations, and Communication Channels

-- 1. Organizations (Companies, Municipal Agencies, Schools, Vendors, Households)
CREATE TABLE IF NOT EXISTS organizations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    organization_type TEXT NOT NULL DEFAULT 'DEFAULT', -- VENDOR, AGENCY, SCHOOL, CLIENT, HOUSEHOLD
    notes TEXT,
    metadata_json TEXT, -- Polymorphic JSON per Implement (precinct, district, vendor tier)
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_organizations_name ON organizations(name);

-- 2. Individual Contacts (People)
CREATE TABLE IF NOT EXISTS contacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    full_name TEXT NOT NULL,
    notes TEXT,
    metadata_json TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_contacts_name ON contacts(full_name);

-- 3. Affiliations (M:N Bridge between Contacts and Organizations)
CREATE TABLE IF NOT EXISTS affiliations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    contact_id INTEGER NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    organization_id INTEGER NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role_title TEXT, -- e.g. "Plant Superintendent", "Principal", "Case Worker", "Lead Tech"
    is_primary INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (contact_id, organization_id)
);
CREATE INDEX IF NOT EXISTS idx_affiliations_org ON affiliations(organization_id);
CREATE INDEX IF NOT EXISTS idx_affiliations_contact ON affiliations(contact_id);

-- 4. Unified Communication Channels (Phone/Email attached to Contact OR Organization)
CREATE TABLE IF NOT EXISTS communication_channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    contact_id INTEGER REFERENCES contacts(id) ON DELETE CASCADE,
    organization_id INTEGER REFERENCES organizations(id) ON DELETE CASCADE,
    channel_type TEXT NOT NULL, -- 'EMAIL', 'PHONE'
    value TEXT NOT NULL,        -- Lowercase email or normalized E.164 phone (+18145550199)
    label TEXT,                 -- 'main', 'switchboard', 'mobile', 'direct', 'billing'
    is_primary INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (
        (contact_id IS NOT NULL AND organization_id IS NULL) OR
        (contact_id IS NULL AND organization_id IS NOT NULL)
    ),
    UNIQUE (channel_type, value)
);
CREATE INDEX IF NOT EXISTS idx_channels_lookup ON communication_channels(channel_type, value);

-- Enforce at most one primary channel per type per contact
CREATE UNIQUE INDEX IF NOT EXISTS idx_contact_channels_one_primary
    ON communication_channels(contact_id, channel_type)
    WHERE is_primary = 1 AND contact_id IS NOT NULL;

-- Enforce at most one primary channel per type per organization
CREATE UNIQUE INDEX IF NOT EXISTS idx_org_channels_one_primary
    ON communication_channels(organization_id, channel_type)
    WHERE is_primary = 1 AND organization_id IS NOT NULL;

-- 5. Relational Bridge on Work Items
ALTER TABLE work_items ADD COLUMN contact_id INTEGER REFERENCES contacts(id) ON DELETE SET NULL;
ALTER TABLE work_items ADD COLUMN organization_id INTEGER REFERENCES organizations(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_work_items_contact ON work_items(contact_id);
CREATE INDEX IF NOT EXISTS idx_work_items_org ON work_items(organization_id);
