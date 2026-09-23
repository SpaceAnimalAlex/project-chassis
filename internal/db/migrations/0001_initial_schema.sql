-- 0001_initial_schema.sql: Project Chassis Canonical Database Schema

-- Users / Staff / Operators
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid TEXT UNIQUE NOT NULL,
    email TEXT UNIQUE NOT NULL,
    full_name TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'AGENT', -- ADMIN, AGENT, READONLY
    is_active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Core Work Queue Item (The Ticket / Work Order / Case)
CREATE TABLE IF NOT EXISTS work_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    item_code TEXT UNIQUE NOT NULL, -- e.g. "WO-1001", "HD-204", "CW-501"
    domain_type TEXT NOT NULL DEFAULT 'IT', -- IT, EDU, CIVIC, MRO
    requester_name TEXT,
    requester_email TEXT NOT NULL,
    assigned_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'NEW', -- NEW, OPEN, PENDING_USER, RESOLVED, CLOSED
    priority TEXT NOT NULL DEFAULT 'NORMAL', -- LOW, NORMAL, HIGH, CRITICAL
    subject TEXT NOT NULL,
    summary TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at DATETIME
);

-- Threaded Messages (Dual-Channel Architecture)
CREATE TABLE IF NOT EXISTS thread_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_item_id INTEGER NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    author_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL, -- NULL if external requester
    sender_email TEXT NOT NULL,
    body TEXT NOT NULL,
    is_internal INTEGER NOT NULL DEFAULT 0, -- 1 = Private note, 0 = Public to requester
    external_message_id TEXT UNIQUE, -- M365 Graph / Gmail Message-ID for idempotent threading
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Asset / Entity Association Registry
CREATE TABLE IF NOT EXISTS assets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_type TEXT NOT NULL, -- HARDWARE, CLASS_SECTION, AGENCY, MACHINE
    identifier TEXT NOT NULL, -- Hostname, Course Code, Serial Number, Parcel ID
    name TEXT NOT NULL,
    metadata_json TEXT, -- Polymorphic JSON attributes validated by Implements
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (domain_type, identifier)
);

-- Work Item <-> Asset Link (Many-to-Many)
CREATE TABLE IF NOT EXISTS work_item_assets (
    work_item_id INTEGER NOT NULL REFERENCES work_items(id) ON DELETE CASCADE,
    asset_id INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    PRIMARY KEY (work_item_id, asset_id)
);

-- Immutable Audit Log
CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    work_item_id INTEGER REFERENCES work_items(id) ON DELETE CASCADE,
    actor_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL, -- ITEM_CREATED, STATUS_CHANGED, REASSIGNED, NOTE_ADDED, PUBLIC_REPLY, ASSET_LINKED, MAIL_CORRELATED
    details_json TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Mail Sync Checkpoints (Ingestion High-Water Marks & Delta Query Tokens)
CREATE TABLE IF NOT EXISTS sync_checkpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL, -- "GRAPH", "GMAIL", "RELAY"
    account_id TEXT NOT NULL, -- Mailbox address or tenant account ID
    delta_token TEXT NOT NULL,
    last_sync_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (provider, account_id)
);

-- Performance Indexes
CREATE INDEX IF NOT EXISTS idx_work_items_status ON work_items(status);
CREATE INDEX IF NOT EXISTS idx_work_items_assigned ON work_items(assigned_user_id);
CREATE INDEX IF NOT EXISTS idx_work_items_domain ON work_items(domain_type);
CREATE INDEX IF NOT EXISTS idx_work_items_created ON work_items(created_at);
CREATE INDEX IF NOT EXISTS idx_thread_messages_item ON thread_messages(work_item_id);
CREATE INDEX IF NOT EXISTS idx_thread_messages_external ON thread_messages(external_message_id);
CREATE INDEX IF NOT EXISTS idx_assets_domain_ident ON assets(domain_type, identifier);
CREATE INDEX IF NOT EXISTS idx_audit_logs_item ON audit_logs(work_item_id);
