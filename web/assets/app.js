const LABELS = {
  cmp_summer_sale: "Summer Sale",
  cmp_welcome: "Welcome series",
  cmp_winback: "Winback",
};

const TYPES = ["sent", "delivered", "opened", "clicked"];
const state = { campaign: "cmp_summer_sale", lastSeed: null, refreshGen: 0 };

const $ = (id) => document.getElementById(id);

function fmt(n) {
  return Number(n || 0).toLocaleString("en-US");
}

function pct(num, den) {
  if (!den) return "—";
  return `${Math.round((num / den) * 1000) / 10}%`;
}

function prettyCampaign(id) {
  return LABELS[id] || id;
}

function tickClock() {
  $("clock").textContent = new Date().toLocaleString();
}

async function api(path, opts) {
  const res = await fetch(path, opts);
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status} ${text}`);
  }
  return res.json();
}

function campaignListHtml(ids) {
  const known = ["cmp_summer_sale", "cmp_welcome", "cmp_winback"];
  const all = [...new Set([...ids, ...known])];
  $("campaign-count").textContent = `${ids.length} with data`;
  $("campaign-list").innerHTML = all
    .map(
      (id) => `
      <li>
        <button type="button" data-id="${id}" class="${id === state.campaign ? "active" : ""}">
          <strong>${prettyCampaign(id)}</strong>
          <span>${id}</span>
        </button>
      </li>`
    )
    .join("");
}

$("campaign-list").addEventListener("click", (e) => {
  const btn = e.target.closest("button");
  if (!btn) return;
  state.campaign = btn.dataset.id;
  refresh();
});

function renderKpis(stats) {
  const events = stats.events || {};
  $("kpis").innerHTML = TYPES.map(
    (t) => `
    <article class="kpi ${t}">
      <div class="label">${t}</div>
      <div class="num">${fmt(events[t])}</div>
    </article>`
  ).join("");
}

function renderCompare(stats) {
  const events = stats.events || {};
  const people = stats.unique_contacts || {};
  const max = Math.max(1, ...TYPES.map((t) => events[t] || 0));
  $("compare").innerHTML = TYPES.map((t) => {
    const ev = events[t] || 0;
    const pe = people[t] || 0;
    return `
      <div>
        <div class="row"><span>${t}</span><div class="bar"><i style="width:${(ev / max) * 100}%;background:var(--${t})"></i></div><b>${fmt(ev)}</b></div>
        <div class="row"><span class="muted">people</span><div class="bar people"><i style="width:${(pe / max) * 100}%;background:var(--${t})"></i></div><span class="muted">${fmt(pe)}</span></div>
      </div>`;
  }).join("");
}

function renderRates(stats) {
  const e = stats.events || {};
  $("rates").innerHTML = `
    <div class="row"><span>Delivery</span><div class="bar"><i style="width:${Math.min(100, (e.delivered / (e.sent || 1)) * 100)}%;background:var(--delivered)"></i></div><b>${pct(e.delivered, e.sent)}</b></div>
    <div class="row"><span>Open</span><div class="bar"><i style="width:${Math.min(100, (e.opened / (e.delivered || 1)) * 100)}%;background:var(--opened)"></i></div><b>${pct(e.opened, e.delivered)}</b></div>
    <div class="row"><span>Click</span><div class="bar"><i style="width:${Math.min(100, (e.clicked / (e.opened || 1)) * 100)}%;background:var(--clicked)"></i></div><b>${pct(e.clicked, e.opened)}</b></div>
    <p class="hint">Opened can exceed delivered. Providers send late and out of order; we still count the open.</p>
  `;
}

function metaText(meta) {
  if (!meta) return "";
  if (typeof meta === "object" && meta.url) return meta.url;
  try {
    return JSON.stringify(meta);
  } catch {
    return "";
  }
}

function renderActivity(page) {
  const rows = page.events || [];
  if (!rows.length) {
    $("activity").innerHTML = `<tr><td class="empty" colspan="5">No stored events yet. Load the provider seed to fill the board.</td></tr>`;
    return;
  }
  $("activity").innerHTML = rows
    .map((ev) => {
      const ts = String(ev.timestamp || "").replace("T", " ").replace("Z", "");
      return `
      <tr>
        <td class="mono">${ts}</td>
        <td><span class="badge ${ev.type}">${ev.type}</span></td>
        <td class="mono">${ev.event_id}</td>
        <td class="mono">${ev.contact_id}</td>
        <td>${metaText(ev.metadata)}</td>
      </tr>`;
    })
    .join("");
}

function showIngest(resp) {
  const el = $("ingest-result");
  el.hidden = false;
  const rejected = (resp.rejected_items || [])
    .slice(0, 4)
    .map((r) => `${r.event_id || "#" + r.index}: ${r.reason}`)
    .join("<br>");
  const conflicts = (resp.payload_conflicts || []).length;
  el.innerHTML = `
    <div>Accepted <b>${resp.accepted}</b> · duplicates <b>${resp.duplicates}</b> · rejected <b>${resp.rejected}</b></div>
    ${conflicts ? `<div>Payload conflicts: <b>${conflicts}</b> (same id, different fields — first write still wins)</div>` : ""}
    ${rejected ? `<div class="muted">${rejected}</div>` : ""}
  `;
  $("link-pill").textContent = "Ingest 200";
  $("link-pill").className = resp.rejected ? "pill warn" : "pill ok";
}

async function refresh() {
  const gen = ++state.refreshGen;
  $("campaign-kicker").textContent = state.campaign;
  $("campaign-title").textContent = prettyCampaign(state.campaign);
  try {
    const [stats, events, list] = await Promise.all([
      api(`/campaigns/${encodeURIComponent(state.campaign)}/stats`),
      api(`/campaigns/${encodeURIComponent(state.campaign)}/events?limit=40`),
      api("/campaigns"),
    ]);
    if (gen !== state.refreshGen) return;
    renderKpis(stats);
    renderCompare(stats);
    renderRates(stats);
    renderActivity(events);
    campaignListHtml(list.campaigns || []);
    if ($("link-pill").textContent === "API idle") {
      $("link-pill").textContent = "Live";
      $("link-pill").className = "pill ok";
    }
  } catch (err) {
    if (gen !== state.refreshGen) return;
    $("link-pill").textContent = "API down";
    $("link-pill").className = "pill warn";
    console.error(err);
  }
}

async function postEvents(batch) {
  const resp = await api("/events", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(batch),
  });
  showIngest(resp);
  await refresh();
}

function setBusy(busy) {
  ["btn-seed", "btn-retry", "btn-live", "btn-conflict"].forEach((id) => {
    $(id).disabled = busy;
  });
}

$("btn-seed").onclick = async () => {
  setBusy(true);
  try {
    const seed = await fetch("/seed/events.json").then((r) => r.json());
    state.lastSeed = seed;
    await postEvents(seed);
  } finally {
    setBusy(false);
  }
};

$("btn-retry").onclick = async () => {
  setBusy(true);
  try {
    const seed = state.lastSeed || (await fetch("/seed/events.json").then((r) => r.json()));
    await postEvents(seed);
  } finally {
    setBusy(false);
  }
};

$("btn-live").onclick = async () => {
  setBusy(true);
  try {
    await postEvents([
      {
        event_id: `evt_live_${Date.now()}`,
        campaign_id: state.campaign,
        contact_id: "ct_demo",
        type: "opened",
        timestamp: new Date().toISOString(),
      },
    ]);
  } finally {
    setBusy(false);
  }
};

$("btn-conflict").onclick = async () => {
  setBusy(true);
  try {
    await postEvents([
      {
        event_id: "evt_00042",
        campaign_id: state.campaign,
        contact_id: "ct_conflict",
        type: "clicked",
        timestamp: "2026-08-10T09:15:04Z",
      },
    ]);
  } finally {
    setBusy(false);
  }
};

tickClock();
setInterval(tickClock, 1000);
refresh();
setInterval(refresh, 4000);
