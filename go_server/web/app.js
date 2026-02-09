const listEl = document.getElementById("alert-list");
const emptyEl = document.getElementById("empty");
const countEl = document.getElementById("stat-count");
const lastEl = document.getElementById("stat-last");
const urlEl = document.getElementById("stat-url");
const autoToggle = document.getElementById("auto");
const loadHistoryBtn = document.getElementById("load-history");
const clearViewBtn = document.getElementById("clear-view");

let openIds = new Set();
let alertsMap = new Map(); // id -> alert
let timerId = null;

function formatDate(value) {
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString();
}

function initURL() {
  const { protocol, hostname, port } = window.location;
  const guessedPort = port || "5123";
  const val = `${protocol}//${hostname || "127.0.0.1"}:${guessedPort}/webhook`;
  urlEl.textContent = val;
  const top = document.getElementById("stat-url-top");
  if (top) top.textContent = val;
}

function buildCard(a) {
  const item = document.createElement("li");
  item.className = "alert-card";
  const rawText = a.raw ? JSON.stringify(a.raw, null, 2) : a.raw_text || "";
  item.dataset.id = a.id;
  item.innerHTML = `
    <div class="pill">${a.alert_type || a.search_name || "Splunk Alert"}</div>
    <div class="host">${a.host || "Unknown host"}</div>
    <div class="meta">
      <div><span>Source IP</span><strong>${a.src_ip || a.src || "—"}</strong></div>
      <div><span>Source</span><strong>${a.source || "—"}</strong></div>
      <div><span>Received</span><strong>${formatDate(a.received_at)}</strong></div>
    </div>
    <div class="type-chip">ID ${a.id} · ${a.search_name || "no name"}</div>
    <details class="raw" data-id="${a.id}">
      <summary>View raw payload</summary>
      <pre>${rawText}</pre>
    </details>
  `;
  const detailsEl = item.querySelector("details");
  detailsEl.addEventListener("toggle", () => {
    if (detailsEl.open) openIds.add(a.id);
    else openIds.delete(a.id);
  });
  if (openIds.has(a.id)) detailsEl.open = true;
  return item;
}

function resetList(alerts) {
  alertsMap.clear();
  listEl.innerHTML = "";
  alerts.forEach((a) => {
    alertsMap.set(a.id, a);
    const card = buildCard(a);
    listEl.appendChild(card);
  });
}

function appendNew(alerts) {
  const newOnes = alerts.filter((a) => !alertsMap.has(a.id));
  for (let i = newOnes.length - 1; i >= 0; i -= 1) {
    const a = newOnes[i]; // oldest new first so newest stays on top
    alertsMap.set(a.id, a);
    const card = buildCard(a);
    listEl.prepend(card);
  }
  return newOnes.length;
}

async function loadAlerts({ forceReset = false } = {}) {
  try {
    const res = await fetch("/api/alerts", { cache: "no-store" });
    if (!res.ok) throw new Error("HTTP " + res.status);
    const data = await res.json();
    const alerts = (data.alerts || []).slice().reverse(); // newest first

    if (forceReset || alerts.length < alertsMap.size) {
      resetList(alerts);
    } else {
      appendNew(alerts);
    }

    const latest = alerts[0];
    countEl.textContent = alerts.length;
    lastEl.textContent = latest ? formatDate(latest.received_at) : "–";
    emptyEl.style.display = alerts.length ? "none" : "block";
  } catch (err) {
    console.error("No se pudieron cargar las alertas:", err);
  }
}

function setAuto(on) {
  if (timerId) {
    clearInterval(timerId);
    timerId = null;
  }
  if (on) {
    timerId = setInterval(() => loadAlerts(), 4000);
  }
}

autoToggle.addEventListener("change", (e) => setAuto(e.target.checked));
loadHistoryBtn.addEventListener("click", async () => {
  try {
    const res = await fetch("/api/history/reload", { method: "POST" });
    if (!res.ok) throw new Error("HTTP " + res.status);
    await loadAlerts({ forceReset: true });
  } catch (err) {
    console.error("Failed to reload history:", err);
  }
});
clearViewBtn.addEventListener("click", () => {
  if (timerId) {
    clearInterval(timerId);
    timerId = null;
    autoToggle.checked = false;
  }
  alertsMap.clear();
  listEl.innerHTML = "";
  openIds.clear();
  countEl.textContent = "0";
  lastEl.textContent = "–";
  emptyEl.style.display = "block";
});

initURL();
loadAlerts({ forceReset: true });
setAuto(autoToggle.checked);
