// Project Chassis — minimal vanilla JS, no build step, no framework.
// Deliberately hand-written rather than vendoring htmx/alpine/etc: this
// ships inside the single binary and the ethos doc asks for standard-library
// / minimal-JS first over framework bloat.
"use strict";

// Populated by initNav() from GET /api/auth/session on every page load.
// Read by initItemPage() (e.g. "assign to me", filtering your own typing
// ping out of the indicator) so those don't need their own round trip.
let currentUser = null;

// Identity comes entirely from the httpOnly session cookie the server sets
// on login (see internal/web/handlers/auth.go) — fetch() sends it
// automatically for same-origin requests, so there's nothing to attach
// client-side. A 401 here always means "no valid session," and is handled
// by bouncing to /login rather than by the caller working around it.
async function api(path, opts = {}) {
  const res = await fetch(path, {
    ...opts,
    headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
  });
  if (res.status === 401 && document.body.dataset.page !== "login") {
    window.location.href = "/login?next=" + encodeURIComponent(window.location.pathname);
    return new Promise(() => {}); // never resolves; we're navigating away
  }
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = (await res.json()).error || msg; } catch { /* body wasn't JSON */ }
    throw new Error(msg);
  }
  if (res.status === 204) return null;
  return res.json();
}

function markConnStatus(ok) {
  const el = document.getElementById("conn-status");
  if (!el) return;
  el.classList.toggle("chassis-status--ok", ok);
  el.classList.toggle("chassis-status--down", !ok);
}

// --- Queue page ---

function initQueuePage() {
  const body = document.getElementById("queue-body");
  const search = document.getElementById("search");
  const statusFilter = document.getElementById("filter-status");
  const orgFilter = document.getElementById("filter-org");
  const orgDatalist = document.getElementById("org-suggestions");
  let rows = [];
  let selected = 0;
  let orgNameToId = {};

  // Seeded from the incoming-call banner's "View ticket(s)" link (see
  // renderIncomingCall in initNav), which can't go through the name-based
  // org filter above since it only knows the organization's id, not its name.
  const initialParams = new URLSearchParams(window.location.search);
  const seededOrgID = initialParams.get("organization_id");

  bindOrgSuggestions(orgFilter, orgDatalist, (orgs) => {
    orgNameToId = {};
    orgs.forEach((o) => { orgNameToId[o.name] = o.id; });
  });
  orgFilter.addEventListener("change", load);

  async function load() {
    const params = new URLSearchParams();
    if (search.value) params.set("q", search.value);
    if (statusFilter.value) params.set("status", statusFilter.value);
    if (orgFilter.value && orgNameToId[orgFilter.value]) {
      params.set("organization_id", orgNameToId[orgFilter.value]);
    } else if (seededOrgID) {
      params.set("organization_id", seededOrgID);
    }
    try {
      rows = await api("/api/queue?" + params.toString());
      markConnStatus(true);
    } catch {
      markConnStatus(false);
      return;
    }
    render();
  }

  function render() {
    if (rows.length === 0) {
      body.innerHTML = '<tr><td colspan="7" class="chassis-empty">Queue is empty.</td></tr>';
      return;
    }
    body.innerHTML = rows.map((r, i) => `
      <tr data-idx="${i}" class="${i === selected ? "chassis-row--selected" : ""}">
        <td>${escapeHtml(r.item_code)}</td>
        <td>${escapeHtml(r.priority)}</td>
        <td>${escapeHtml(r.subject)}</td>
        <td>${escapeHtml(r.requester_name || r.requester_email)}</td>
        <td>${escapeHtml(r.assigned_name || "—")}</td>
        <td>${escapeHtml(r.status)}</td>
        <td>${new Date(r.updated_at).toLocaleString()}</td>
      </tr>
    `).join("");
    [...body.querySelectorAll("tr")].forEach((tr) => {
      tr.addEventListener("click", () => openSelected(Number(tr.dataset.idx)));
    });
  }

  function openSelected(idx) {
    const row = rows[idx];
    if (row) window.location.href = "/items/" + row.id;
  }

  function move(delta) {
    if (rows.length === 0) return;
    selected = Math.max(0, Math.min(rows.length - 1, selected + delta));
    render();
    body.children[selected]?.scrollIntoView({ block: "nearest" });
  }

  document.addEventListener("keydown", (e) => {
    if (document.activeElement === search) {
      if (e.key === "Escape") search.blur();
      return;
    }
    switch (e.key) {
      case "j": move(1); break;
      case "k": move(-1); break;
      case "Enter": case "o": openSelected(selected); break;
      case "/": e.preventDefault(); search.focus(); break;
      default: return;
    }
  });

  search.addEventListener("input", debounce(load, 250));
  statusFilter.addEventListener("change", load);

  load();
  setInterval(load, 15000); // passive refresh; not a substitute for presence
}

// --- Item detail page ---

function initItemPage() {
  const id = Number(window.location.pathname.split("/").pop());
  const thread = document.getElementById("thread");
  const composerBody = document.getElementById("composer-body");
  const isInternal = document.getElementById("is-internal");
  const sendBtn = document.getElementById("send-btn");
  const statusSelect = document.getElementById("status-select");
  const assignBtn = document.getElementById("assign-btn");
  const lockWarning = document.getElementById("lock-warning");
  const promoteBtn = document.getElementById("promote-btn");
  const promoteForm = document.getElementById("promote-form");
  const promoteOrgInput = document.getElementById("promote-org");
  const promoteRoleInput = document.getElementById("promote-role");
  const promoteOrgDatalist = document.getElementById("org-suggestions");

  const TRANSITIONS = ["NEW", "OPEN", "PENDING_USER", "RESOLVED", "CLOSED"];

  async function load() {
    try {
      const detail = await api(`/api/items/${id}`);
      markConnStatus(true);
      renderItem(detail);
    } catch {
      markConnStatus(false);
    }
  }

  function renderItem(detail) {
    document.getElementById("item-code").textContent = detail.item.item_code;
    document.getElementById("item-status").textContent = detail.item.status;
    document.getElementById("item-requester").textContent =
      `${detail.item.requester_name || ""} <${detail.item.requester_email}>`;
    document.getElementById("item-subject").textContent = detail.item.subject;

    statusSelect.innerHTML = TRANSITIONS.map(
      (s) => `<option value="${s}" ${s === detail.item.status ? "selected" : ""}>${s}</option>`
    ).join("");

    const messages = detail.messages || [];
    thread.innerHTML = messages.length
      ? messages.map(renderMessage).join("")
      : '<p class="chassis-empty">No messages yet.</p>';

    // Once a requester is linked to a contact, there's nothing left to
    // promote — hide the action rather than let a second click re-promote
    // (PromoteRequesterToContact is written to be safe either way, but the
    // button implies "do this," not "do this again").
    promoteBtn.hidden = !!detail.item.contact_id;
    if (detail.item.contact_id) promoteForm.hidden = true;
  }

  function renderMessage(m) {
    const who = m.is_internal ? (m.author_name || "Staff") : (m.author_name || m.sender_email);
    const lock = m.is_internal ? '<span class="chassis-message__lock">🔒 internal</span>' : "";
    return `
      <div class="chassis-message ${m.is_internal ? "chassis-message--internal" : ""}">
        <div class="chassis-message__meta">
          <strong>${escapeHtml(who)}</strong>
          <span>${new Date(m.created_at).toLocaleString()}</span>
          ${lock}
        </div>
        <div class="chassis-message__body">${escapeHtml(m.body)}</div>
      </div>
    `;
  }

  statusSelect.addEventListener("change", async () => {
    try {
      await api(`/api/items/${id}/transition`, {
        method: "POST",
        body: JSON.stringify({ new_status: statusSelect.value }),
      });
      await load();
    } catch (e) {
      alert("Could not change status: " + e.message);
      await load();
    }
  });

  assignBtn.addEventListener("click", async () => {
    try {
      await api(`/api/items/${id}/assign`, {
        method: "POST",
        body: JSON.stringify({ target_user_id: currentUser?.id ?? null }),
      });
      await load();
    } catch (e) {
      alert("Could not assign: " + e.message);
    }
  });

  bindOrgSuggestions(promoteOrgInput, promoteOrgDatalist);

  promoteBtn.addEventListener("click", () => {
    promoteForm.hidden = !promoteForm.hidden;
  });

  document.getElementById("promote-cancel-btn").addEventListener("click", () => {
    promoteForm.hidden = true;
  });

  document.getElementById("promote-confirm-btn").addEventListener("click", async () => {
    try {
      await api(`/api/items/${id}/promote-to-contact`, {
        method: "POST",
        body: JSON.stringify({
          organization_name: promoteOrgInput.value.trim(),
          role_title: promoteRoleInput.value.trim(),
        }),
      });
      promoteForm.hidden = true;
      promoteOrgInput.value = "";
      promoteRoleInput.value = "";
      await load();
    } catch (e) {
      alert("Could not promote requester: " + e.message);
    }
  });

  sendBtn.addEventListener("click", async () => {
    const body = composerBody.value.trim();
    if (!body) return;
    try {
      await api(`/api/items/${id}/messages`, {
        method: "POST",
        body: JSON.stringify({ body, is_internal: isInternal.checked }),
      });
      composerBody.value = "";
      await load();
      heartbeat("VIEWING");
    } catch (e) {
      alert("Could not send: " + e.message);
    }
  });

  // --- presence / collision prevention ---

  async function heartbeat(mode) {
    try {
      const res = await api(`/api/items/${id}/presence`, {
        method: "POST",
        body: JSON.stringify({ mode }),
      });
      const others = (res && res.others) || [];
      if (others.length > 0) {
        const names = others.map((o) => `${o.user_name} (${o.mode.toLowerCase()})`).join(", ");
        lockWarning.textContent = `⚠ Also here: ${names} — coordinate before sending to avoid a duplicate reply.`;
        lockWarning.hidden = false;
      } else {
        lockWarning.hidden = true;
      }
    } catch {
      // presence is a courtesy signal; a failed heartbeat shouldn't block work
    }
  }

  composerBody.addEventListener("focus", () => heartbeat("DRAFTING"));
  composerBody.addEventListener("blur", () => heartbeat("VIEWING"));

  const heartbeatTimer = setInterval(() => {
    heartbeat(document.activeElement === composerBody ? "DRAFTING" : "VIEWING");
  }, 10000);

  // --- live stream: new messages + typing indicator (native EventSource,
  // no third-party SSE library — see internal/web/stream for the server side) ---

  const typingEl = document.getElementById("typing-indicator");
  let typingClearTimer = null;
  const TYPING_DISPLAY_MS = 2500; // matches stream.TypingTTL server-side

  const evtSource = new EventSource(`/api/items/${id}/stream`);

  evtSource.addEventListener("message", () => {
    // A new message landed (staff reply or inbound email) — reload the
    // thread rather than hand-splicing the event payload in, so the queue
    // status badge (a reply can flip PENDING_USER<->OPEN) stays correct too.
    load();
  });

  evtSource.addEventListener("typing", (e) => {
    const ping = JSON.parse(e.data);
    if (currentUser && ping.user_id === currentUser.id) return; // don't show yourself typing
    typingEl.textContent = `${ping.user_name} is typing…`;
    typingEl.hidden = false;
    clearTimeout(typingClearTimer);
    typingClearTimer = setTimeout(() => { typingEl.hidden = true; }, TYPING_DISPLAY_MS);
  });

  evtSource.onerror = () => markConnStatus(false);
  evtSource.onopen = () => markConnStatus(true);

  const sendTyping = throttle(() => {
    api(`/api/items/${id}/typing`, { method: "POST" }).catch(() => {});
  }, 1200);

  composerBody.addEventListener("input", () => {
    if (composerBody.value.trim()) sendTyping();
  });

  window.addEventListener("beforeunload", () => {
    clearInterval(heartbeatTimer);
    evtSource.close();
    navigator.sendBeacon?.(`/api/items/${id}/presence`, new Blob());
    // sendBeacon can't set method to DELETE; best-effort release only.
  });

  load();
  heartbeat("VIEWING");
}

// --- Login page ---

function initLoginPage() {
  const form = document.getElementById("login-form");
  const errorEl = document.getElementById("login-error");
  const params = new URLSearchParams(window.location.search);

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    errorEl.hidden = true;
    try {
      await api("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({
          email: document.getElementById("login-email").value,
          password: document.getElementById("login-password").value,
        }),
      });
      window.location.href = params.get("next") || "/";
    } catch (e) {
      errorEl.textContent = e.message || "Sign-in failed";
      errorEl.hidden = false;
    }
  });
}

// --- Shared nav: current user + multi-device session management ---
// Signing in on a second device never signs the first one out (see
// core.AuthService.CreateSession) — this panel is where an operator can see
// every device currently holding a session and kill the ones that
// shouldn't still be signed in (e.g. a lost phone).

async function initNav() {
  const userEl = document.getElementById("nav-user");
  const logoutBtn = document.getElementById("nav-logout-btn");
  const devicesBtn = document.getElementById("nav-devices-btn");
  const devicesPanel = document.getElementById("devices-panel");
  const devicesList = document.getElementById("devices-list");
  const logoutOthersBtn = document.getElementById("logout-others-btn");
  if (!userEl) return; // login page has no nav

  try {
    const me = await api("/api/auth/session");
    currentUser = me;
    userEl.textContent = me.full_name || me.email;
  } catch {
    return; // api() already redirected to /login on 401
  }

  logoutBtn.addEventListener("click", async () => {
    await api("/api/auth/logout", { method: "POST" });
    window.location.href = "/login";
  });

  devicesBtn.addEventListener("click", async () => {
    const open = !devicesPanel.hidden;
    if (open) {
      devicesPanel.hidden = true;
      return;
    }
    const sessions = await api("/api/auth/sessions");
    devicesList.innerHTML = sessions.map((s) => `
      <li>
        <span>${escapeHtml(s.device_label || "Unknown device")}${s.is_current ? " (this device)" : ""}</span>
        <span>${new Date(s.last_seen_at).toLocaleString()}</span>
      </li>
    `).join("") || "<li>No other active sessions.</li>";
    devicesPanel.hidden = false;
  });

  logoutOthersBtn?.addEventListener("click", async () => {
    await api("/api/auth/logout-others", { method: "POST" });
    devicesPanel.hidden = true;
  });

  openOperatorStream();
}

// --- Operator-wide live stream: incoming-call screen-pop ---
// One EventSource per page load, opened from the app shell (not per-ticket
// like the item-detail stream) — see internal/web/stream.Hub.operatorSubs
// and handlers.VoiceIncoming for the server side.

function openOperatorStream() {
  const es = new EventSource("/api/stream/operator");
  es.addEventListener("incoming_call", (e) => renderIncomingCall(JSON.parse(e.data)));
  // Best-effort: a dropped operator stream doesn't affect markConnStatus,
  // which reflects the per-item stream's health on the ticket detail page.
}

function renderIncomingCall(call) {
  const banner = document.getElementById("incoming-call-banner");
  const text = document.getElementById("incoming-call-text");
  const viewLink = document.getElementById("incoming-call-view-link");
  if (!banner) return;

  let label;
  if (call.contact) {
    label = call.contact.full_name;
    if (call.primary_affiliation) {
      label += ` — ${call.primary_affiliation.organization_name}` +
        (call.primary_affiliation.role_title ? ` (${call.primary_affiliation.role_title})` : "");
    }
  } else if (call.organization) {
    label = `${call.organization.name} (main line)`;
  } else {
    label = "Unknown caller";
  }
  text.textContent = `☎ ${label} — ${call.caller_number}`;

  const items = call.active_work_items || [];
  if (items.length === 1) {
    viewLink.href = `/items/${items[0].id}`;
    viewLink.textContent = "View ticket";
    viewLink.hidden = false;
  } else if (items.length > 1 && call.organization) {
    viewLink.href = `/?organization_id=${call.organization.id}`;
    viewLink.textContent = `View ${items.length} tickets`;
    viewLink.hidden = false;
  } else {
    viewLink.hidden = true;
  }

  banner.hidden = false;
}

// --- shared helpers ---

// bindOrgSuggestions wires a text input to a <datalist>, populated from
// GET /api/organizations?q= as the user types. Used by both the queue page's
// organization filter and the item page's "Promote to Contact" form — the
// only two places in the UI that need an organization typeahead, so this
// stays a small shared helper rather than a new component. onResults is
// optional and receives the raw org objects (queue filter uses it to build a
// name->id map for the organization_id query param; the promote form doesn't
// need one, since it submits the organization by name).
function bindOrgSuggestions(input, datalist, onResults) {
  input.addEventListener("input", debounce(async () => {
    const q = input.value.trim();
    if (!q) {
      datalist.innerHTML = "";
      onResults?.([]);
      return;
    }
    try {
      const orgs = await api("/api/organizations?q=" + encodeURIComponent(q));
      datalist.innerHTML = orgs.map((o) => `<option value="${escapeHtml(o.name)}"></option>`).join("");
      onResults?.(orgs);
    } catch {
      // typeahead is a courtesy; a failed lookup shouldn't block typing
    }
  }, 250));
}

function escapeHtml(s) {
  const div = document.createElement("div");
  div.textContent = s ?? "";
  return div.innerHTML;
}

function debounce(fn, ms) {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), ms);
  };
}

// Unlike debounce, throttle fires on the leading edge and then ignores
// further calls until ms has passed — what the typing ping wants (send
// immediately when typing starts, then at most once per interval), not a
// "wait until they stop" debounce.
function throttle(fn, ms) {
  let blocked = false;
  return (...args) => {
    if (blocked) return;
    blocked = true;
    fn(...args);
    setTimeout(() => { blocked = false; }, ms);
  };
}

document.addEventListener("DOMContentLoaded", () => {
  const page = document.body.dataset.page;
  if (page === "login") { initLoginPage(); return; }
  document.getElementById("incoming-call-dismiss-btn")?.addEventListener("click", () => {
    document.getElementById("incoming-call-banner").hidden = true;
  });
  initNav();
  if (page === "queue") initQueuePage();
  if (page === "item") initItemPage();
});
