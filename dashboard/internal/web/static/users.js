// pispot-ui — Users tab: list, add, change and delete accounts.
// The server enforces every rule here; the disabled controls only reflect them.
(() => {
  "use strict";

  const MIN_PASSWORD = 8;
  const STATUS_MS = 6000;
  const $ = (id) => document.getElementById(id);

  let loaded = false;
  let statusTimer = null;

  function el(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  async function api(method, path, body) {
    const options = { method, cache: "no-store", credentials: "include" };
    if (body !== undefined) {
      options.headers = { "Content-Type": "application/json" };
      options.body = JSON.stringify(body);
    }
    const r = await fetch(path, options);
    const data = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(data.error || `HTTP ${r.status}`);
    return data;
  }

  const userPath = (name) => `/api/users/${encodeURIComponent(name)}`;

  function passwordProblem(password, confirm) {
    if (password.length < MIN_PASSWORD) return `Passwords must be at least ${MIN_PASSWORD} characters.`;
    if (password !== confirm) return "The passwords don't match.";
    return "";
  }

  function setStatus(message) {
    clearTimeout(statusTimer);
    $("users-status").textContent = message;
    if (message) {
      statusTimer = setTimeout(() => { $("users-status").textContent = ""; }, STATUS_MS);
    }
  }

  async function change(action, successMessage) {
    $("users-error").textContent = "";
    setStatus("");
    try {
      await action();
      setStatus(successMessage);
    } catch (e) {
      $("users-error").textContent = e.message;
    }
    await load();
  }

  function checkboxCell(u, label, checked) {
    const box = el("input");
    box.type = "checkbox";
    box.checked = checked;
    box.setAttribute("aria-label", `${u.username}: ${label}`);
    const cell = el("td");
    cell.append(box);
    return [cell, box];
  }

  function adminCheckbox(u, isMe) {
    const [cell, box] = checkboxCell(u, "admin", u.admin);
    if (u.owner) {
      box.disabled = true;
      box.title = "Owners can only be changed over SSH";
    } else if (isMe) {
      box.disabled = true;
      box.title = "You can't remove your own admin access";
    }
    box.addEventListener("change", async () => {
      // A second click can't send another change while this one is saving.
      box.disabled = true;
      const message = box.checked ? `${u.username} is now an admin.` : `Removed admin from ${u.username}.`;
      await change(() => api("PATCH", userPath(u.username), { admin: box.checked }), message);
      box.disabled = false;
    });
    return cell;
  }

  // Owner is never changed from this page, on any account.
  function ownerCheckbox(u) {
    const [cell, box] = checkboxCell(u, "owner", Boolean(u.owner));
    box.disabled = true;
    box.title = "Owner can only be changed over SSH";
    return cell;
  }

  function passwordRow(u, isMe) {
    const row = el("tr", "users-password-row");
    row.hidden = true;
    const cell = el("td");
    cell.colSpan = 4;
    const form = el("form", "users-form");
    form.autocomplete = "off";

    const input = (placeholder) => {
      const i = el("input", "wifi-input");
      i.type = "password";
      i.placeholder = placeholder;
      i.autocomplete = "new-password";
      i.required = true;
      return i;
    };
    const password = input(`New password for ${u.username}`);
    const confirm = input("Confirm password");
    const save = el("button", "ctrl-btn", "Set password");
    save.type = "submit";
    form.append(password, confirm, save);

    cell.append(form);
    if (isMe) cell.append(el("div", "sub", "Your browser will ask you to log in again."));
    row.append(cell);

    form.addEventListener("submit", (e) => {
      e.preventDefault();
      const problem = passwordProblem(password.value, confirm.value);
      if (problem) {
        $("users-error").textContent = problem;
        return;
      }
      const value = password.value;
      password.value = "";
      confirm.value = "";
      change(() => api("PATCH", userPath(u.username), { password: value }), `Password changed for ${u.username}.`);
    });
    return row;
  }

  function renderUser(u, me) {
    const isMe = u.username === me;
    const row = el("tr");
    const name = el("td", "mono", u.username);
    if (isMe) name.append(el("span", "sub", " (you)"));
    row.append(name);
    row.append(adminCheckbox(u, isMe), ownerCheckbox(u));

    // Owners have no Password or Delete buttons: the server refuses both.
    if (u.owner) {
      row.append(el("td"));
      return [row];
    }

    const pwRow = passwordRow(u, isMe);
    const actions = el("div", "users-actions");
    const pw = el("button", "ctrl-btn", "Password");
    pw.type = "button";
    pw.addEventListener("click", () => { pwRow.hidden = !pwRow.hidden; });
    const del = el("button", "ctrl-btn", "Delete");
    del.type = "button";
    if (isMe) {
      del.disabled = true;
      del.title = "You can't delete the account you're logged in as";
    } else {
      del.addEventListener("click", () => {
        if (!window.confirm(`Delete user "${u.username}"? Their home folder will be kept.`)) return;
        change(() => api("DELETE", userPath(u.username)), `Deleted ${u.username}. Their home folder was kept.`);
      });
    }
    actions.append(pw, del);
    const actionCell = el("td");
    actionCell.append(actions);
    row.append(actionCell);
    return [row, pwRow];
  }

  async function load() {
    try {
      const data = await api("GET", "/api/users");
      $("users-rows").replaceChildren(...data.users.flatMap((u) => renderUser(u, data.me)));
      loaded = true;
    } catch (e) {
      $("users-error").textContent = `Couldn't load users: ${e.message}`;
    }
  }

  async function addUser(e) {
    e.preventDefault();
    const errorEl = $("user-add-error");
    errorEl.textContent = "";
    setStatus("");
    const password = $("add-password").value;
    const problem = passwordProblem(password, $("add-confirm").value);
    if (problem) {
      errorEl.textContent = problem;
      return;
    }
    const username = $("add-username").value.trim();
    const admin = $("add-admin").checked;
    try {
      await api("POST", "/api/users", { username, password, admin });
      $("user-add").reset();
      setStatus(admin ? `Added ${username} as an admin.` : `Added ${username}.`);
      await load();
    } catch (err) {
      errorEl.textContent = err.message;
    }
  }

  function init() {
    $("user-add").addEventListener("submit", addUser);
    document.addEventListener("pispot:tab", (e) => {
      if (e.detail === "users" && !loaded) load();
    });
  }

  init();
})();
