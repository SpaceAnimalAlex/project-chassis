# Project Chassis

An anti-extractive operations engine. Single static Go binary, embedded SQLite, zero recurring rent, zero mandatory cloud dependency. Deploy it once, own your data, and let it run for a decade.

---

## The Bedrock: Why We Build

Project Chassis is not a theoretical exercise in minimal design. It is built to honor a lineage of working people who refused to be broken by extractive systems.

### The Land They Left
Around 1910, two young immigrant lines made the transatlantic crossing to escape old-world systems engineered to extract labor from the working class while denying them ownership:

* **Peter Hillebrand & Maria (Mary) Zöschg (South Tyrol):** A young couple who left the steep alpine valleys of a fracturing South Tyrol, squeezed by agrarian poverty, mountain land plots subdivided to the brink of starvation, and impending imperial conscription on the eve of the First World War.
* **Peter & Amanda Anderson (Sweden):** Left an agrarian society where the rigid *statare* and *torpare* tenant systems legally bound landless families to seasonal farm labor, harsh parish scrutiny, and perpetual poverty.

Both couples staked everything on the belief that a working family should own their tools, control their hearth, and keep the fruits of their toil.

### The Crucible of Elk County, Pennsylvania
They landed in the hemlock and mountain valleys of Elk County, Pennsylvania: a rugged industrial frontier already shaped by an understated, stoic Quaker civic framework. But industrial Pennsylvania was no gentle refuge; it was an unforgiving forge:

* **The Meat Grinder:** They traded the old-world landlord for the company town. They labored in the caustic acid of the leather tanneries, the sulfur smoke of the paper mills, and the dark drift mines. Later, in places like Saint Marys and Ridgway, they fed the carbon bake ovens and powdered-metal presses of Speer and Stackpole, breathing black graphite dust so the nation could electrify.
* **Surviving the 1930s:** When the Great Depression shattered the industrial economy, they did not wait for institutional rescue. Peter and Mary raised nine children; Peter and Amanda raised four. Thirteen young lives were carried through the collapse not by corporate benevolence, but through kitchen-table frugality, hillside potato patches, hunting the ridge woods, and tight-knit mutual aid.
* **The Toll of the Golden Fifties:** The postwar boom finally brought steady paychecks, but it was paid for in human iron. Blown discs, industrial lung disease, ringing ears from the stamping presses, and swing-shift exhaustion were common. They absorbed the physical beating of the industrial machine so the generations following them would not have to break their bodies just to survive.

### The Modern Parallels
Every generation fights a different variation of the same extractive trap:
* The 1910s was agrarian subjugation and imperial drafts.
* The 1930s was economic ruin and industrial exploitation.
* The 1950s was corporate lock-in and physical attrition.
* **Today is digital feudalism.**

The modern enterprise software cartel operates exactly like the old-world estate and the company store. They demand perpetual recurring rent just to access your own operational records. They charge arbitrary per-seat taxes that penalize independent organizations for putting people to work. They engineer artificial complexity, phone home telemetry, and revoke access the moment an arbitrary toll is not paid.

### The Mandate
Project Chassis rejects the digital company store:

1. **Own the Machine:** A single static Go binary with embedded SQLite, running locally on a $300 box with zero recurring subscriptions.
2. **Built for Durability:** Cast-iron engineering over disposable framework fads. Code written once that will compile and run a decade from now without intervention.
3. **Operational Sovereignty:** Returning agency to independent workers, small community shops, local teachers, and civic advocates.

This software is dedicated to Peter Hillebrand and Maria (Mary) Zöschg, to Peter and Amanda Anderson, and to every worker in the mills, mines, and forests of Elk County who endured the brutal weight of their era to buy their descendants freedom.

We do not build to become digital sharecroppers. We build tools that outlast the landlord.

---

## What This Is

Chassis is a self-hosted operational queue: help-desk tickets, work orders, casework, whatever an org needs to triage and track. Instead of a monolithic ERP, it uses the **Implement Pattern** — a shared core engine (queues, threading, presence, mail) with thin domain packages layered on top:

* **Chassis IT** — help desk triage, workstation/device registers
* **Chassis Edu** — rosters, assignment submission, private rubric grading
* **Chassis Civic** — constituent casework, municipal agency routing
* **Chassis MRO / Trades** — field service work orders, preventative maintenance

Core mechanics:

* **Dual-channel threading** — every message on a ticket is either an internal staff note or a public reply to the requester, never ambiguously both. Enforced server-side, not by client convention.
* **Deterministic state machine** — `NEW → OPEN → PENDING_USER → RESOLVED/CLOSED`, with automatic transitions on staff/requester replies.
* **Live presence & collision prevention** — operators see who else is viewing or drafting a reply on the same ticket before they duplicate the work.
* **Real-time updates** — a live SSE stream pushes new messages and a typing indicator to anyone with a ticket open, whether the message came from a staff reply or an inbound email.
* **Real multi-device auth** — bcrypt-hashed passwords, server-side sessions (one per device, no cap), "sign out of all other devices" for a lost/stolen device.
* **Mail integration without illusions** — Microsoft Graph / Google Workspace APIs instead of fighting raw SMTP blacklists, while all ticket data and internal notes stay on the local machine.

## Architecture

```
cmd/chassis/          entrypoint: config, migrations, wiring, graceful shutdown
internal/core/         domain models + state machine — no DB, no HTTP
internal/implement/    the Implement Pattern (IT, Edu, Civic, MRO)
internal/db/           SQLite (WAL, modernc.org/sqlite pure-Go), migrations, repository
internal/mailengine/   MailProvider interface + Graph/Gmail/Relay adapters + sync worker
internal/presence/     in-memory viewer/collision tracker
internal/web/           HTTP routing, auth middleware, embedded UI (vanilla JS, no framework)
internal/web/stream/    SSE broadcast hub
internal/web/session/   session cookie mechanics
```

Built by two collaborating agents (Claude and Antigravity/Gemini) under a Lead Architect, with a shared architectural alignment log (`chassis_architecture_collaboration_log.md`) and file-level work-tracking board (`chassis_file_structure_log.md`) kept alongside the code.

## Getting Started

Requires Go 1.24+ (developed against 1.27).

```sh
go build ./cmd/chassis
```

First run needs a bootstrap admin, since there's no other way to get a working login on a fresh database:

```sh
CHASSIS_BOOTSTRAP_ADMIN_EMAIL=you@example.com \
CHASSIS_BOOTSTRAP_ADMIN_PASSWORD=change-me \
./chassis
```

Then visit `http://localhost:8080`.

### Configuration (environment variables)

| Variable | Default | Purpose |
|---|---|---|
| `CHASSIS_DB_PATH` | `chassis.db` | SQLite database file |
| `CHASSIS_ADDR` | `:8080` | HTTP listen address |
| `CHASSIS_VERBOSE` | off | Debug-level logging |
| `CHASSIS_BOOTSTRAP_ADMIN_EMAIL` / `_PASSWORD` / `_NAME` | — | First-run admin account (only used when the `users` table is empty) |
| `CHASSIS_ITEM_PREFIX` | `HD` | Item code prefix for mail-ingested tickets |
| `CHASSIS_GRAPH_TENANT_ID` / `_CLIENT_ID` / `_CLIENT_SECRET` / `_MAILBOX` | — | Microsoft Graph mail sync (optional — Chassis runs fine as a local-only queue without it) |

## Status

* **Mail providers:** Microsoft Graph done; Gmail and local SMTP/IMAP relay adapters not yet built.
* **Implements:** IT, Edu, Civic, and MRO are registered and validate their own asset metadata.
* **Auth:** password + session-based, multi-device by design, no role-based UI restrictions yet beyond the `ADMIN`/`AGENT`/`READONLY` roles on the model.
