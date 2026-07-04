"use strict";

// ---- tiny DOM + fetch helpers -------------------------------------------------

function h(tag, attrs, ...children) {
  const e = document.createElement(tag);
  if (attrs) {
    for (const [k, v] of Object.entries(attrs)) {
      if (v == null) continue;
      if (k === "class") e.className = v;
      else if (k === "html") e.innerHTML = v;
      else if (k.startsWith("on") && typeof v === "function") e.addEventListener(k.slice(2), v);
      else e.setAttribute(k, v);
    }
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    e.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
  }
  return e;
}

async function api(path, opts = {}) {
  const init = { method: opts.method || "GET", headers: {}, credentials: "same-origin" };
  if (opts.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(opts.body);
  }
  const resp = await fetch(path, init);
  let data = null;
  try { data = await resp.json(); } catch (_) {}
  if (!resp.ok) {
    const msg = (data && data.error) || ("HTTP " + resp.status);
    throw new Error(msg);
  }
  return data;
}

let toastTimer = null;
function toast(msg, isErr) {
  const t = document.getElementById("toast");
  t.textContent = msg;
  t.className = "toast show" + (isErr ? " err" : "");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { t.className = "toast"; }, 3200);
}

// ---- app state + routing ------------------------------------------------------

let state = null;
let nav = "dashboard";
let wizResume = false; // true when running the switch+cloud steps for an existing vault
const app = () => document.getElementById("app");

function setView(node) {
  const root = app();
  root.innerHTML = "";
  root.appendChild(node);
}

async function refresh() {
  state = await api("/api/state");
  document.getElementById("version").textContent = state.version ? "v" + state.version : "";
  render();
}

function render() {
  if (!state.configured) return viewWizard();
  if (!state.unlocked) return viewUnlock();
  if (nav === "wizard") return viewWizard(); // resumed switch/cloud setup for an existing vault
  if (nav === "entries") return viewEntries();
  return viewDashboard();
}

// startSwitchSetup enters the wizard at the dead-man's-switch step for a vault that
// already exists but has no switch configured.
function startSwitchSetup() {
  wizResume = true;
  wiz = { step: 2, secrets: null };
  nav = "wizard";
  render();
}

function exitWizardToDashboard() {
  wizResume = false;
  wiz = { step: 0, secrets: null };
  nav = "dashboard";
  refresh();
}

// appShell wraps unlocked-state content with the top navigation.
function appShell(active, ...content) {
  const tab = (id, label) => h("button", {
    class: "navtab" + (active === id ? " active" : ""), type: "button",
    onclick: () => { nav = id; render(); }
  }, label);
  return h("div", null,
    h("div", { class: "navbar" }, tab("dashboard", "Dashboard"), tab("entries", "Entries")),
    ...content);
}

// ---- unlock -------------------------------------------------------------------

function viewUnlock() {
  const err = h("div", { class: "error" });
  const pw = h("input", { type: "password", id: "pw", placeholder: "Vault password", autofocus: "true" });
  const btn = h("button", { class: "btn", type: "submit" }, "Unlock");

  const form = h("form", {
    onsubmit: async (e) => {
      e.preventDefault();
      err.textContent = "";
      btn.disabled = true;
      try {
        state = await api("/api/unlock", { method: "POST", body: { password: pw.value } });
        render();
      } catch (ex) {
        err.textContent = ex.message;
        btn.disabled = false;
        pw.focus(); pw.select();
      }
    }
  },
    h("h1", null, "Unlock your vault"),
    h("p", { class: "sub" }, "Enter your daily vault password to manage entries and the dead man's switch."),
    h("label", null, "Password"),
    pw, err,
    h("div", { class: "btn-row" }, btn)
  );

  setView(h("div", { class: "card card-narrow" }, form));
  pw.focus();
}

// ---- dashboard ----------------------------------------------------------------

function pill(on, onLabel, offLabel, warn) {
  const cls = on ? (warn ? "pill pill-warn" : "pill pill-ok") : "pill pill-off";
  return h("span", { class: cls }, on ? onLabel : offLabel);
}

function viewDashboard() {
  const overdue = state.checkinInterval > 0 && state.daysSince >= 0 && state.daysSince > state.checkinInterval;
  const stats = h("div", { class: "status-grid" },
    stat("Vault", state.vaultDir || "—"),
    stat("Entries", String(state.entryCount)),
    statNode("Last check-in", h("span", null,
      state.lastCheckin || "never",
      state.daysSince >= 0 ? h("span", { class: "muted" }, "  (" + state.daysSince + "d ago)") : null)),
    statNode("Dead man's switch",
      state.switchConfigured
        ? h("span", null, pill(true, state.cloudOnly ? "cloud-only" : "armed", "", false))
        : pill(false, "", "not configured", false))
  );

  const checkinBtn = h("button", { class: "btn", type: "button", onclick: doCheckin }, "Check in now");

  setView(appShell("dashboard",
    h("div", { class: "card" },
      h("h1", null, "Dashboard"),
      h("p", { class: "sub" }, overdue
        ? "⚠ Your check-in is overdue — check in to keep the switch from firing."
        : "Everything looks healthy. Check in regularly to keep the switch quiet."),
      stats,
      h("div", { class: "btn-row" },
        checkinBtn,
        h("span", { class: "spacer" }),
        state.switchConfigured
          ? h("button", { class: "btn btn-ghost", type: "button", onclick: doVerify }, "Verify switch")
          : h("button", { class: "btn", type: "button", onclick: startSwitchSetup }, "Set up dead man's switch")
      )
    )
  ));
}

// ---- entries ------------------------------------------------------------------

const CAT_LABEL = { notes: "Note", credentials: "Credential", documents: "Document" };

function viewEntries() {
  setView(appShell("entries", h("div", { class: "loading" }, "Loading entries…")));
  loadEntries();
}

async function loadEntries() {
  try {
    const entries = await api("/api/entries");
    renderEntriesList(entries);
  } catch (ex) {
    setView(appShell("entries", h("div", { class: "card" }, h("p", { class: "error" }, ex.message))));
  }
}

function renderEntriesList(entries) {
  const list = entries.length === 0
    ? h("p", { class: "muted" }, "No entries yet. Add your first note or credential.")
    : h("div", { class: "entry-list" },
      entries.map((e) => h("button", { class: "entry-row", type: "button", onclick: () => openEntry(e.id) },
        h("span", { class: "entry-cat" }, CAT_LABEL[e.category] || e.category),
        h("span", { class: "entry-title" }, e.title || e.name),
        h("span", { class: "entry-date muted" }, (e.updatedAt || "").slice(0, 10)))));

  setView(appShell("entries",
    h("div", { class: "card" },
      h("div", { class: "list-head" },
        h("h1", null, "Entries"),
        h("div", { class: "btn-row-inline" },
          h("button", { class: "btn btn-sm", type: "button", onclick: () => renderEntryEditor("notes", null) }, "+ Note"),
          h("button", { class: "btn btn-sm", type: "button", onclick: () => renderEntryEditor("credentials", null) }, "+ Credential"))),
      list)));
}

async function openEntry(id) {
  try {
    const e = await api("/api/entries/" + encodeURIComponent(id));
    renderEntryDetail(e);
  } catch (ex) { toast(ex.message, true); }
}

function renderEntryDetail(e) {
  let bodyNode;
  if (e.category === "notes") {
    bodyNode = h("pre", { class: "note-body" }, e.content || "");
  } else if (e.category === "credentials") {
    const c = e.credential || {};
    bodyNode = h("div", { class: "cred-fields" },
      credRow("Service", c.service), credRow("URL", c.url),
      credRow("Username", c.username), credRow("Password", c.password, true),
      credRow("Notes", c.notes));
  } else {
    bodyNode = h("p", { class: "muted" }, "Binary document (" + (e.size || 0) + " bytes). Manage documents with the CLI.");
  }

  const canEdit = e.category === "notes" || e.category === "credentials";
  setView(appShell("entries",
    h("div", { class: "card" },
      h("div", { class: "list-head" },
        h("h1", null, e.title || e.name),
        h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: () => loadEntries() }, "← Back")),
      h("div", { class: "muted" }, CAT_LABEL[e.category] || e.category),
      bodyNode,
      h("div", { class: "btn-row" },
        canEdit ? h("button", { class: "btn", type: "button", onclick: () => renderEntryEditor(e.category, e) }, "Edit") : null,
        h("span", { class: "spacer" }),
        h("button", { class: "btn btn-danger", type: "button", onclick: () => deleteEntry(e) }, "Delete")))));
}

function credRow(label, value, secret) {
  if (!value) return null;
  const val = h("span", { class: "mono" + (secret ? " secret-mask" : "") }, value);
  return h("div", { class: "cred-row" },
    h("span", { class: "cred-k" }, label),
    val,
    h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: (ev) => copy(value, ev.target) }, "Copy"),
    secret ? h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: (ev) => { val.classList.toggle("secret-mask"); ev.target.textContent = val.classList.contains("secret-mask") ? "Show" : "Hide"; } }, "Show") : null);
}

function renderEntryEditor(category, entry) {
  const isNew = !entry;
  const err = h("div", { class: "error" });
  const fields = {};
  let form;

  if (category === "notes") {
    const title = h("input", { type: "text", id: "ntitle", value: entry ? (entry.title || "") : "", placeholder: "Note title" });
    const content = h("textarea", { id: "ncontent", placeholder: "Write your note (Markdown supported)" });
    content.value = entry ? (entry.content || "") : "";
    fields.get = () => ({ category: "notes", title: title.value, content: content.value });
    form = [h("label", null, "Title"), title, h("label", null, "Content"), content];
    if (!isNew) title.disabled = true; // title is fixed after creation (filename-derived)
  } else {
    const c = (entry && entry.credential) || {};
    const mk = (id, ph, val, type) => (fields[id] = h("input", { id, type: type || "text", placeholder: ph, value: val || "" }));
    form = [
      h("label", null, "Service"), mk("service", "e.g. Bank", c.service),
      h("label", null, "URL"), mk("url", "https://…", c.url),
      h("label", null, "Username"), mk("username", "user@example.com", c.username),
      h("label", null, "Password"), mk("password", "", c.password, "text"),
      h("label", null, "Notes"), (fields.notes = h("textarea", { id: "cnotes", placeholder: "(optional)" })),
    ];
    fields.notes.value = c.notes || "";
    if (!isNew) fields.service.disabled = true;
    fields.get = () => ({
      category: "credentials",
      credential: { service: fields.service.value, url: fields.url.value, username: fields.username.value, password: fields.password.value, notes: fields.notes.value }
    });
  }

  const btn = h("button", { class: "btn", type: "submit" }, isNew ? "Create" : "Save");
  const el = h("form", {
    onsubmit: async (e) => {
      e.preventDefault(); err.textContent = ""; btn.disabled = true;
      try {
        const body = fields.get();
        if (isNew) await api("/api/entries", { method: "POST", body });
        else await api("/api/entries/" + encodeURIComponent(entry.id), { method: "PUT", body });
        toast(isNew ? "Entry created." : "Entry saved.");
        loadEntries();
      } catch (ex) { err.textContent = ex.message; btn.disabled = false; }
    }
  },
    h("div", { class: "list-head" },
      h("h1", null, (isNew ? "New " : "Edit ") + (CAT_LABEL[category] || category).toLowerCase()),
      h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: () => loadEntries() }, "← Cancel")),
    ...form, err,
    h("div", { class: "btn-row" }, btn));

  setView(appShell("entries", h("div", { class: "card" }, el)));
}

async function deleteEntry(e) {
  if (!confirm('Delete "' + (e.title || e.name) + '"? This cannot be undone.')) return;
  try {
    await api("/api/entries/" + encodeURIComponent(e.id), { method: "DELETE" });
    toast("Entry deleted.");
    loadEntries();
  } catch (ex) { toast(ex.message, true); }
}

function stat(k, v) { return statNode(k, h("span", null, v)); }
function statNode(k, vNode) {
  return h("div", { class: "stat" }, h("div", { class: "k" }, k), h("div", { class: "v" }, vNode));
}

async function doCheckin(e) {
  const btn = e.target; btn.disabled = true;
  try {
    const r = await api("/api/checkin", { method: "POST" });
    toast(r.pushed ? "Checked in and pushed to the cloud." : "Checked in locally.");
    await refresh();
  } catch (ex) {
    toast(ex.message, true); btn.disabled = false;
  }
}

async function doVerify(e) {
  const btn = e.target; btn.disabled = true;
  try {
    const r = await api("/api/switch/verify", { method: "POST" });
    toast(r.ok ? "Switch verified — armed and current." : "Switch has warnings — see details.", !r.ok);
  } catch (ex) {
    toast(ex.message, true);
  } finally { btn.disabled = false; }
}

// ---- setup wizard -------------------------------------------------------------

const WIZ_STEPS = ["Create vault", "Save secrets", "Dead man's switch", "Cloud (GitHub)", "Package", "Done"];
let wiz = { step: 0, secrets: null };

function viewWizard() {
  if (wiz.step === 1 && !wiz.secrets) wiz.step = 0; // lost secrets after reload
  const steps = [wizCreate, wizSecrets, wizSwitch, wizCloud, wizPackage, wizDone];
  setView(h("div", null, wizProgress(), steps[wiz.step]()));
}

function wizProgress() {
  return h("div", { class: "wizard-steps" },
    WIZ_STEPS.map((label, i) =>
      h("div", { class: "wstep" + (i === wiz.step ? " active" : "") + (i < wiz.step ? " done" : "") },
        h("span", { class: "wstep-num" }, i < wiz.step ? "✓" : String(i + 1)),
        h("span", { class: "wstep-label" }, label))));
}

function wizGoto(step) { wiz.step = step; viewWizard(); }

// Step 1: create the vault.
function wizCreate() {
  const err = h("div", { class: "error" });
  const pw = h("input", { type: "password", id: "wpw", placeholder: "Choose a strong password" });
  const pw2 = h("input", { type: "password", id: "wpw2", placeholder: "Repeat password" });
  const dir = h("input", { type: "text", id: "wdir", placeholder: "(default) ~/kawarimi-vault" });
  const btn = h("button", { class: "btn", type: "submit" }, "Create vault");

  const form = h("form", {
    onsubmit: async (e) => {
      e.preventDefault();
      err.textContent = "";
      if (pw.value.length < 8) { err.textContent = "Use at least 8 characters."; return; }
      if (pw.value !== pw2.value) { err.textContent = "Passwords do not match."; return; }
      btn.disabled = true;
      try {
        wiz.secrets = await api("/api/init", { method: "POST", body: { password: pw.value, vaultDir: dir.value.trim() } });
        wizGoto(1);
      } catch (ex) { err.textContent = ex.message; btn.disabled = false; }
    }
  },
    h("h1", null, "Create your vault"),
    h("p", { class: "sub" }, "This password unlocks the vault for daily use on this device."),
    h("label", null, "Password"), pw,
    h("label", null, "Confirm password"), pw2,
    h("label", null, "Vault folder ", h("span", { class: "hint" }, "(optional)")), dir,
    err,
    h("div", { class: "btn-row" }, btn)
  );
  return h("div", { class: "card card-narrow" }, form);
}

// Step 2: one-time secrets.
function wizSecrets() {
  const s = wiz.secrets;
  const saved = h("input", { type: "checkbox", id: "savedChk" });
  const cont = h("button", { class: "btn", type: "button", disabled: "true" },
    "Continue to the dead man's switch");
  saved.addEventListener("change", () => { cont.disabled = !saved.checked; });
  cont.addEventListener("click", () => wizGoto(2));

  return h("div", { class: "card" },
    h("h1", null, "Write these down now"),
    h("p", { class: "sub" }, "These are shown only once. Store them safely and do not reload this page until you have saved them."),
    secretBlock("Mnemonic words", "Your personal backup — store in a safe.",
      h("div", { class: "word-grid" },
        s.mnemonic.map((wd, i) => h("div", { class: "word" }, h("span", { class: "word-i" }, String(i + 1)), wd))),
      s.mnemonic.join(" ")),
    secretBlock("Recovery code", "Regain access if you lose this device.",
      h("div", { class: "secret-val mono" }, s.recoveryCode), s.recoveryCode),
    secretBlock("Recipient passphrase", "Print this on a card and give it to your recipients.",
      h("div", { class: "secret-val mono" }, s.recipientPassphrase), s.recipientPassphrase),
    h("div", { class: "btn-row" },
      h("button", { class: "btn btn-ghost", type: "button", onclick: () => window.print() }, "Print"),
      h("span", { class: "spacer" }),
      h("label", { class: "inline-check" }, saved, " I have saved these securely")),
    h("div", { class: "btn-row" }, cont)
  );
}

function secretBlock(title, sub, valNode, copyText) {
  return h("div", { class: "secret-box" },
    h("div", { class: "secret-head" },
      h("div", null, h("div", { class: "secret-title" }, title), h("div", { class: "muted" }, sub)),
      h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: (e) => copy(copyText, e.target) }, "Copy")),
    valNode);
}

function copy(text, btn) {
  navigator.clipboard.writeText(text).then(() => {
    const old = btn.textContent; btn.textContent = "Copied"; setTimeout(() => { btn.textContent = old; }, 1200);
  }).catch(() => toast("Copy failed — select and copy manually.", true));
}

// Step 3: dead man's switch settings.
function wizSwitch() {
  const err = h("div", { class: "error" });
  const f = {};
  const inp = (id, attrs) => (f[id] = h("input", Object.assign({ id }, attrs)));
  const btn = h("button", { class: "btn", type: "submit" }, "Save & continue");

  const form = h("form", {
    onsubmit: async (e) => {
      e.preventDefault();
      err.textContent = "";
      btn.disabled = true;
      const body = {
        smtpServer: f.smtpServer.value, smtpPort: parseInt(f.smtpPort.value) || 587,
        smtpUsername: f.smtpUsername.value, smtpPassword: f.smtpPassword.value,
        senderEmail: f.senderEmail.value, userEmail: f.userEmail.value,
        recipients: f.recipients.value.split(/[,\n]/).map((x) => x.trim()).filter(Boolean),
        warning1Days: parseInt(f.w1.value) || 0, warning2Days: parseInt(f.w2.value) || 0,
        finalDays: parseInt(f.final.value) || 0,
        vaultPackageLocation: f.pkgLoc.value,
        telegramBotToken: f.tgToken.value, telegramChatId: f.tgChat.value,
        imapServer: f.imapServer.value, imapPort: parseInt(f.imapPort.value) || 0,
        localRelease: document.querySelector('input[name="release"]:checked').value === "local",
      };
      try {
        await api("/api/switch/setup", { method: "POST", body });
        wizGoto(3);
      } catch (ex) { err.textContent = ex.message; btn.disabled = false; }
    }
  },
    h("h2", null, "Email (SMTP)"),
    h("p", { class: "muted" }, "Used to warn you and to notify recipients when the switch fires."),
    h("div", { class: "row" },
      field("SMTP server", inp("smtpServer", { type: "text", placeholder: "smtp.gmail.com" })),
      field("Port", inp("smtpPort", { type: "number", value: "587" }))),
    h("div", { class: "row" },
      field("SMTP username", inp("smtpUsername", { type: "text", placeholder: "you@gmail.com" })),
      field("SMTP password", inp("smtpPassword", { type: "password", placeholder: "app password" }))),
    field("Sender email (optional)", inp("senderEmail", { type: "text", placeholder: "defaults to username" })),

    h("h2", null, "Recipients & timing"),
    field("Your email (for warnings)", inp("userEmail", { type: "email", placeholder: "you@example.com" })),
    fieldNode("Recipient emails", "one per line or comma-separated",
      (f.recipients = h("textarea", { id: "recipients", placeholder: "family@example.com" }))),
    h("div", { class: "row" },
      field("Warning 1 (days)", inp("w1", { type: "number", value: "14" })),
      field("Warning 2 (days)", inp("w2", { type: "number", value: "21" })),
      field("Final release (days)", inp("final", { type: "number", value: "30" }))),
    field("Vault package location", inp("pkgLoc", { type: "text", placeholder: "Drive/GitHub link where recipients download the package" })),

    h("h2", null, "Optional channels"),
    h("div", { class: "row" },
      field("Telegram bot token", inp("tgToken", { type: "text", placeholder: "(optional)" })),
      field("Telegram chat ID", inp("tgChat", { type: "text", placeholder: "(optional)" }))),
    h("div", { class: "row" },
      field("IMAP server", inp("imapServer", { type: "text", placeholder: "(optional) reply-to-checkin" })),
      field("IMAP port", inp("imapPort", { type: "number", placeholder: "993" }))),

    h("h2", null, "Final release mode"),
    h("label", { class: "inline-check" },
      h("input", { type: "radio", name: "release", value: "cloud", checked: "true" }),
      " Cloud only ", h("span", { class: "hint" }, "(recommended — this machine holds no key)")),
    h("label", { class: "inline-check" },
      h("input", { type: "radio", name: "release", value: "local" }),
      " Also allow release from this machine"),
    err,
    h("div", { class: "btn-row" },
      h("button", { class: "btn btn-ghost", type: "button", onclick: () => wizResume ? exitWizardToDashboard() : wizGoto(1) }, wizResume ? "Cancel" : "Back"),
      h("span", { class: "spacer" }), btn)
  );
  return h("div", { class: "card" }, h("h1", null, "Dead man's switch"), form);
}

// Step 4: GitHub cloud automation.
function wizCloud() {
  const err = h("div", { class: "error" });
  const tok = h("input", { type: "password", id: "ghtok", placeholder: "ghp_… (needs 'repo' scope)" });
  const repo = h("input", { type: "text", id: "ghrepo", value: "kawarimi-dms" });
  const btn = h("button", { class: "btn", type: "submit" }, "Create repo & arm the switch");

  const form = h("form", {
    onsubmit: async (e) => {
      e.preventDefault();
      err.textContent = "";
      btn.disabled = true; btn.textContent = "Working… (this can take a few seconds)";
      try {
        const r = await api("/api/switch/cloud", { method: "POST", body: { githubToken: tok.value, repoName: repo.value.trim() } });
        wiz.cloud = r;
        wizGoto(4);
      } catch (ex) { err.textContent = ex.message; btn.disabled = false; btn.textContent = "Create repo & arm the switch"; }
    }
  },
    h("h1", null, "Arm the cloud switch"),
    h("p", { class: "sub" }, "kawarimi will create a private GitHub repo, set its Actions secrets, and push the workflow that emails your recipients if you stop checking in."),
    h("p", { class: "muted" }, "Create a token at github.com → Settings → Developer settings → Personal access tokens, with the 'repo' scope. Your SSH key must also be registered with GitHub (used to push heartbeats)."),
    h("label", null, "GitHub personal access token"), tok,
    h("label", null, "New private repo name"), repo,
    err,
    h("div", { class: "btn-row" },
      h("button", { class: "btn btn-ghost", type: "button", onclick: () => wizGoto(2) }, "Back"),
      h("span", { class: "spacer" }), btn)
  );
  return h("div", { class: "card card-narrow" }, form);
}

// Step 5: build the recipient package.
function wizPackage() {
  const err = h("div", { class: "error" });
  const out = h("input", { type: "text", id: "pkgout", placeholder: "(default) ~/kawarimi-vault.zip" });
  const result = h("div", { class: "muted" });
  const btn = h("button", { class: "btn", type: "submit" }, "Build package");

  const form = h("form", {
    onsubmit: async (e) => {
      e.preventDefault();
      err.textContent = ""; result.textContent = "";
      btn.disabled = true; btn.textContent = "Building…";
      const mode = document.querySelector('input[name="pkgmode"]:checked').value;
      try {
        const r = await api("/api/package/build", { method: "POST", body: { mode, output: out.value.trim() } });
        result.innerHTML = "";
        result.appendChild(h("div", { class: "ok-line" }, "✓ Built " + r.path + " (" + r.sizeMB + " MB)"));
        btn.textContent = "Rebuild";
        btn.disabled = false;
      } catch (ex) { err.textContent = ex.message; btn.disabled = false; btn.textContent = "Build package"; }
    }
  },
    h("h1", null, "Build the recipient package"),
    h("p", { class: "sub" }, "A zip with the encrypted vault and instructions — no secrets. Upload it to the location you set above."),
    h("label", { class: "inline-check" },
      h("input", { type: "radio", name: "pkgmode", value: "auto", checked: "true" }),
      " Include recipient apps ", h("span", { class: "hint" }, "(cross-compiles from source)")),
    h("label", { class: "inline-check" },
      h("input", { type: "radio", name: "pkgmode", value: "none" }),
      " No apps ", h("span", { class: "hint" }, "(recipients install kawarimi themselves)")),
    h("label", null, "Output file (optional)"), out,
    err, result,
    h("div", { class: "btn-row" },
      h("button", { class: "btn btn-ghost", type: "button", onclick: () => wizGoto(3) }, "Back"),
      h("span", { class: "spacer" }),
      btn,
      h("button", { class: "btn", type: "button", onclick: () => wizGoto(5) }, "Finish")))
  ;
  return h("div", { class: "card" }, form);
}

// Step 6: done.
function wizDone() {
  return h("div", { class: "card card-narrow" },
    h("h1", null, "You're all set"),
    h("p", { class: "sub" }, "Your vault is created and the cloud dead man's switch is armed. Add entries and check in from the dashboard. Remember to hand the recipient card to your recipients and upload the package to its location."),
    h("div", { class: "btn-row" },
      h("button", { class: "btn", type: "button", onclick: exitWizardToDashboard }, "Go to dashboard"))
  );
}

// small field helpers
function field(label, node) { return fieldNode(label, null, node); }
function fieldNode(label, hint, node) {
  return h("div", null, h("label", null, label, hint ? h("span", { class: "hint" }, "  " + hint) : null), node);
}

// ---- lifecycle ----------------------------------------------------------------

function startKeepalive() {
  setInterval(() => { api("/api/ping").catch(() => {}); }, 30000);
}

document.getElementById("quitBtn").addEventListener("click", async () => {
  if (!confirm("Stop the kawarimi console? You can restart it with 'kawarimi gui'.")) return;
  try { await api("/api/quit", { method: "POST" }); } catch (_) {}
  document.body.innerHTML =
    '<div style="padding:60px;text-align:center;color:#888">kawarimi console stopped. You can close this tab.</div>';
});

(async function main() {
  startKeepalive();
  try {
    await refresh();
  } catch (ex) {
    setView(h("div", { class: "card" }, h("h1", null, "Cannot reach the console"), h("p", { class: "error" }, ex.message)));
  }
})();
