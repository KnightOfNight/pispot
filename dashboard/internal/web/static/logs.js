// pispot-ui — Logs tab: health summary and collapsible command output.
// Data loads when the tab is first opened and on Refresh, not on a timer.
(() => {
  "use strict";

  const STORE_KEY = "pispot_logs_open";
  const SUMMARY_ID = "summary";
  const STATUS_LABELS = { ok: "OK", warn: "WARN", bad: "PROBLEM", info: "INFO" };

  const $ = (id) => document.getElementById(id);

  let loaded = false;
  let loading = false;

  // Sections stay open or closed across refreshes and page reloads.
  function loadOpenIds() {
    try {
      const saved = JSON.parse(window.localStorage.getItem(STORE_KEY));
      if (Array.isArray(saved)) return new Set(saved);
    } catch (e) {
      // Storage unavailable or unreadable: use the default below.
    }
    return new Set([SUMMARY_ID]);
  }

  const openIds = loadOpenIds();

  function saveOpenIds() {
    try {
      window.localStorage.setItem(STORE_KEY, JSON.stringify([...openIds]));
    } catch (e) {
      // Only a convenience, so a failure here doesn't matter.
    }
  }

  function el(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function makeSection(id, title, command) {
    const details = el("details", "log-section");
    details.dataset.id = id;
    details.open = openIds.has(id);
    details.addEventListener("toggle", () => {
      if (details.open) openIds.add(id);
      else openIds.delete(id);
      saveOpenIds();
    });
    const summary = el("summary");
    summary.append(title);
    if (command) summary.append(el("span", "log-command mono", command));
    details.append(summary);
    return details;
  }

  function renderChecks(checks) {
    const problems = checks.filter((c) => c.status === "warn" || c.status === "bad").length;
    const title = problems === 0 ? "Health summary: no problems found"
      : `Health summary: ${problems} to check`;
    const details = makeSection(SUMMARY_ID, title);
    const table = el("table", "check-table");
    const body = el("tbody");
    for (const c of checks) {
      const row = el("tr");
      row.append(el("td", "", c.name));
      const status = el("td");
      status.append(el("span", `check-status ${c.status}`, STATUS_LABELS[c.status] || c.status));
      row.append(status);
      row.append(el("td", "", c.detail));
      body.append(row);
    }
    table.append(body);
    details.append(table);
    return details;
  }

  function renderSection(s) {
    const details = makeSection(s.id, s.title, s.command);
    if (s.truncated) details.append(el("div", "log-note", "Showing the most recent output only."));
    if (s.error) details.append(el("div", "log-note", `Couldn't run this: ${s.error}`));
    details.append(el("pre", "log-output", s.output || (s.error ? "" : "(no output)")));
    return details;
  }

  function render(report) {
    const content = $("logs-content");
    const nodes = [renderChecks(report.checks || [])];
    let group = null;
    for (const s of report.sections || []) {
      if (s.group !== group) {
        group = s.group;
        nodes.push(el("h3", "logs-group-title", group));
        if (group === "Logs") {
          nodes.push(el("div", "summary", "This boot only. Times before the clock syncs can be wrong, because the Pi has no battery-backed clock by default."));
        }
      }
      nodes.push(renderSection(s));
    }
    content.replaceChildren(...nodes);
    $("logs-updated").textContent = `Updated ${new Date(report.generated_at).toLocaleString()}`;
  }

  async function load() {
    if (loading) return;
    loading = true;
    const button = $("logs-refresh");
    button.disabled = true;
    button.textContent = "Loading...";
    $("logs-error").textContent = "";
    try {
      const r = await fetch("/api/logs", { cache: "no-store", credentials: "include" });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      render(await r.json());
      loaded = true;
    } catch (e) {
      $("logs-error").textContent = `Couldn't load logs: ${e.message}`;
    } finally {
      loading = false;
      button.disabled = false;
      button.textContent = "Refresh";
    }
  }

  function setAll(open) {
    for (const details of $("logs-content").querySelectorAll("details.log-section")) {
      details.open = open;
    }
  }

  function init() {
    $("logs-expand").addEventListener("click", () => setAll(true));
    $("logs-collapse").addEventListener("click", () => setAll(false));
    $("logs-refresh").addEventListener("click", load);
    document.addEventListener("pispot:tab", (e) => {
      if (e.detail === "logs" && !loaded) load();
    });
  }

  init();
})();
