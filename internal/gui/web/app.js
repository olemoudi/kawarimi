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
  const el = document.getElementById("toast");
  el.textContent = msg;
  el.className = "toast show" + (isErr ? " err" : "");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.className = "toast"; }, 3800);
}

// ---- i18n -----------------------------------------------------------------------

// Two full dictionaries; `lang` comes from localStorage, defaulting to the browser
// language (Spanish-speaking users get Spanish, everyone else English). The topbar
// toggle switches and persists it. fmt() fills {0},{1}… placeholders.
const I18N = {
  en: {
    ownerConsole: "owner console",
    quit: "Quit",
    themeTitle: "Theme",
    langTitle: "Idioma: cambiar a español",
    loading: "Loading…",
    cannotReach: "Cannot reach the console",
    quitConfirm: "Stop the kawarimi console? You can restart it by running kawarimi again.",
    quitDone: "kawarimi console stopped. You can close this tab.",
    navDashboard: "Dashboard",
    navEntries: "Entries",

    unlockTitle: "Unlock your vault",
    unlockSub: "Enter your vault password to manage entries and the dead man's switch.",
    password: "Password",
    unlockBtn: "Unlock",
    vaultPassword: "Vault password",

    eyebrowStatus: "Switch status",
    stNotArmed: "NOT ARMED",
    stArmed: "ARMED",
    stQuiet: "ARMED · QUIET",
    stOverdue: "OVERDUE",
    stReleasing: "RELEASING",
    noteNotSetup: "The dead man's switch is not set up yet.",
    noteNoCheckin: "No check-in recorded yet — check in once to start the clock.",
    notePastRelease: "Past the release threshold — the cloud may already have delivered the key. Check in NOW if you are seeing this.",
    noteOverdueUrgent: "Final release on day {0}. Check in now.",
    noteOverdueWarn: "Warnings are being sent. Final release on day {0}.",
    noteQuiet1: "Checked in {0} day ago. Nothing will be sent.",
    noteQuietN: "Checked in {0} days ago. Nothing will be sent.",
    tickDay0: "day 0",
    tickWarn1: "warn 1 · {0}d",
    tickWarn2: "warn 2 · {0}d",
    tickRelease: "release · {0}d",
    checkinBtn: "Check in now",
    setupSwitchBtn: "Set up the dead man's switch",
    verifyBtn: "Verify switch",
    factEntries: "Entries",
    factLastCheckin: "Last check-in",
    factReleaseMode: "Release mode",
    factVault: "Vault",
    modeCloudOnly: "cloud-only",
    modeCloudPlus: "cloud + this machine",
    never: "never",
    toastCheckinCloud: "Checked in — timer reset, cloud updated.",
    toastCheckinLocal: "Checked in — timer reset.",
    toastCheckinCloudFail: "Checked in locally, but the cloud switch was NOT updated: {0}",
    toastVerifyOk: "Switch verified — armed and current.",
    toastVerifyWarn: "Switch needs attention — run 'kawarimi switch verify' for details.",

    entriesTitle: "Entries",
    addNote: "+ Note",
    addCredential: "+ Credential",
    emptyVault: "Nothing in the vault yet. What you add here is what your recipients receive.",
    writeNote: "Write a note",
    addCredentialLong: "Add a credential",
    loadingEntries: "Loading entries…",
    catNote: "Note",
    catCredential: "Credential",
    catDocument: "Document",
    back: "← Back",
    cancel: "← Cancel",
    edit: "Edit",
    del: "Delete",
    delConfirm: "Delete “{0}”? This cannot be undone.",
    binaryDoc: "Binary document ({0} bytes). Manage documents with the CLI.",
    fService: "Service", fURL: "URL", fUsername: "Username", fPassword: "Password", fNotes: "Notes",
    copyBtn: "Copy", copied: "Copied", show: "Show", hide: "Hide",
    copyFail: "Copy failed — select and copy manually.",
    newNote: "New note", editNote: "Edit note", newCred: "New credential", editCred: "Edit credential",
    noteTitle: "Title", noteContent: "Content",
    notePh: "Note title", noteContentPh: "Write your note (Markdown supported)",
    credServicePh: "e.g. Bank", credNotesPh: "(optional)",
    create: "Create", save: "Save",
    toastCreated: "Entry created.", toastSaved: "Entry saved.", toastDeleted: "Entry deleted.",
    errTitleRequired: "A title is required.", errServiceRequired: "A service name is required.",

    wizSteps: ["Create vault", "Save secrets", "Dead man's switch", "Cloud (GitHub)", "Package", "Done"],
    wCreateTitle: "Create your vault",
    wCreateSub: "This password unlocks the vault for daily use on this device. Pick something strong you can remember — you will also get a recovery code and a paper backup in the next step.",
    wConfirmPw: "Confirm password",
    wFolder: "Vault folder",
    wOptional: "(optional)",
    wFolderPh: "(default) ~/kawarimi-vault",
    wPwPh: "Choose a strong password",
    wPw2Ph: "Repeat password",
    wCreateBtn: "Create vault",
    errPwShort: "Use at least 8 characters.",
    errPwMismatch: "Passwords do not match.",
    errPwWeak: "This password is too weak — pick a stronger one, or tick the box to use it anyway.",
    strength0: "very weak", strength1: "weak", strength2: "fair", strength3: "strong", strength4: "excellent",
    strengthLine: "{0} — a $100k/year attacker would crack it in {1}",
    wPwWeakChk: "Use this weak password anyway (not recommended)",
    durLtHour: "under an hour", durHours: "about {0} hours", durDays: "about {0} days",
    durMonths: "about {0} months", durYears: "about {0} years", durKYears: "about {0} thousand years",
    durMYears: "about {0} million years", durBYears: "over a billion years",

    wSecretsTitle: "Write these down now",
    wSecretsSub: "These are shown only once. Store them safely and do not reload this page until you have saved them.",
    wMnemonic: "Mnemonic words",
    wMnemonicSub: "Your personal backup — store on paper, in a safe place.",
    wRecovery: "Recovery code",
    wRecoverySub: "Regain access if you lose this device.",
    wCard: "Recipient passphrase",
    wCardSub: "Print this on a card and give it to your recipients. They will need it — and it alone opens nothing.",
    print: "Print",
    savedCheck: "I have saved these securely",
    wSecretsNext: "Continue to the dead man's switch",

    wSwitchTitle: "Dead man's switch",
    wSmtpH: "Email (SMTP)",
    wSmtpSub: "kawarimi sends warnings to you — and, one day, the delivery email to your recipients — through your own email account.",
    wSmtpHelp: "Gmail: your normal password will not work. Create an “App Password” at myaccount.google.com/apppasswords and paste it below. Ports 587 and 465 both work.",
    wSmtpServer: "SMTP server", wSmtpPort: "Port", wSmtpUser: "SMTP username", wSmtpPass: "SMTP password",
    wSmtpPassPh: "app password",
    wSender: "Sender email", wSenderHint: "defaults to the username",
    wRecipH: "Recipients & timing",
    wYourEmail: "Your email (for warnings)",
    wRecipients: "Recipient emails",
    wRecipientsHint: "one per line or comma-separated — who receives the vault",
    wTimingHelp: "If you stop checking in: day {0} — you get a warning; day {1} — urgent warnings; day {2} — the key is emailed to your recipients. Any check-in resets the clock to day 0.",
    wW1: "Warning 1 (days)", wW2: "Warning 2 (days)", wFinal: "Final release (days)",
    wPkgLoc: "Vault package location",
    wPkgLocPh: "Link where recipients will download the package",
    wPkgLocHelp: "Where you will upload the package built in step 5 — a Google Drive / Dropbox shared link, a web address, anything your recipients can open years from now. This link is included in the delivery email.",
    wOptH: "Optional channels",
    wTgToken: "Telegram bot token", wTgChat: "Telegram chat ID",
    wImapServer: "IMAP server", wImapPort: "IMAP port",
    wImapPh: "(optional) check in by replying to emails",
    wModeH: "Final release mode",
    wModeCloud: "Cloud only",
    wModeCloudHint: "(recommended — this machine holds no key)",
    wModeLocal: "Also allow release from this machine",
    wSaveNext: "Save & continue",
    backBtn: "Back",
    errThresholds: "Days must increase: warning 1 < warning 2 < final release.",

    wCloudTitle: "Arm the cloud switch",
    wCloudSub: "kawarimi creates a private GitHub repository, sets its secrets, and installs the automation that emails your recipients if you stop checking in. It runs on GitHub even when your computer is off.",
    wCloudHelp: "You need a GitHub account and a token: create one at github.com/settings/tokens/new?scopes=repo (the link preselects the “repo” scope), set any expiration, and paste it below. The token is used once, only to set things up, and is never stored.",
    wCloudTokenLink: "Create the token (opens GitHub)",
    wGhToken: "GitHub personal access token",
    wGhRepo: "New private repo name",
    wCloudBtn: "Create repo & arm the switch",
    wCloudWorking: "Working… (this can take a few seconds)",

    wPkgTitle: "Build the recipient package",
    wPkgSub: "A zip with the encrypted vault and instructions — no secrets inside. Upload it to the location you set in step 3.",
    wPkgAuto: "Include recipient apps",
    wPkgAutoHint: "(builds the program for every platform; needs the source checkout)",
    wPkgNone: "No apps",
    wPkgNoneHint: "(recipients download kawarimi themselves)",
    wPkgOut: "Output file",
    wPkgOutPh: "(default) ~/kawarimi-vault.zip",
    wPkgBuild: "Build package",
    wPkgBuilding: "Building…",
    wPkgRebuild: "Rebuild",
    wPkgBuilt: "✓ Built {0} ({1} MB)",
    finish: "Finish",

    wDoneTitle: "You're all set",
    wDoneSub: "Your vault is created and the cloud dead man's switch is armed. Add entries and check in from the dashboard. Remember to hand each recipient their card and to upload the package to its location.",
    goDashboard: "Go to dashboard",

    updateAvailable: "Update available: v{0}",
    updateNow: "Update now",
    whatsNew: "What's new",
    updating: "Updating…",
    updatedRestart: "Updated to v{0}. Close kawarimi and open it again to use the new version.",
    updateFailed: "Update failed: {0}",
  },

  es: {
    ownerConsole: "consola del titular",
    quit: "Salir",
    themeTitle: "Tema",
    langTitle: "Language: switch to English",
    loading: "Cargando…",
    cannotReach: "No se puede conectar con la consola",
    quitConfirm: "¿Detener la consola de kawarimi? Puedes volver a abrirla ejecutando kawarimi de nuevo.",
    quitDone: "Consola de kawarimi detenida. Puedes cerrar esta pestaña.",
    navDashboard: "Panel",
    navEntries: "Contenido",

    unlockTitle: "Desbloquea tu caja fuerte",
    unlockSub: "Introduce la contraseña de la caja fuerte para gestionar el contenido y el interruptor.",
    password: "Contraseña",
    unlockBtn: "Desbloquear",
    vaultPassword: "Contraseña de la caja fuerte",

    eyebrowStatus: "Estado del interruptor",
    stNotArmed: "SIN ARMAR",
    stArmed: "ARMADA",
    stQuiet: "ARMADA · EN SILENCIO",
    stOverdue: "ATRASADA",
    stReleasing: "ENTREGANDO",
    noteNotSetup: "El interruptor de hombre muerto aún no está configurado.",
    noteNoCheckin: "Aún no hay señales de vida registradas — da una primera señal para poner en marcha el reloj.",
    notePastRelease: "Superado el umbral de entrega — puede que la nube ya haya enviado la clave. Da señales de vida AHORA si estás viendo esto.",
    noteOverdueUrgent: "Entrega final el día {0}. Da señales de vida ahora.",
    noteOverdueWarn: "Se están enviando avisos. Entrega final el día {0}.",
    noteQuiet1: "Diste señales de vida hace {0} día. No se enviará nada.",
    noteQuietN: "Diste señales de vida hace {0} días. No se enviará nada.",
    tickDay0: "día 0",
    tickWarn1: "aviso 1 · {0}d",
    tickWarn2: "aviso 2 · {0}d",
    tickRelease: "entrega · {0}d",
    checkinBtn: "Dar señales de vida",
    setupSwitchBtn: "Configurar el interruptor",
    verifyBtn: "Verificar interruptor",
    factEntries: "Elementos",
    factLastCheckin: "Última señal",
    factReleaseMode: "Modo de entrega",
    factVault: "Caja fuerte",
    modeCloudOnly: "solo nube",
    modeCloudPlus: "nube + este equipo",
    never: "nunca",
    toastCheckinCloud: "Señal registrada — reloj a cero, nube actualizada.",
    toastCheckinLocal: "Señal registrada — reloj a cero.",
    toastCheckinCloudFail: "Señal registrada en este equipo, pero la nube NO se actualizó: {0}",
    toastVerifyOk: "Interruptor verificado — armado y al día.",
    toastVerifyWarn: "El interruptor necesita atención — ejecuta 'kawarimi switch verify' para ver detalles.",

    entriesTitle: "Contenido",
    addNote: "+ Nota",
    addCredential: "+ Credencial",
    emptyVault: "La caja fuerte está vacía. Lo que añadas aquí es lo que recibirán tus destinatarios.",
    writeNote: "Escribir una nota",
    addCredentialLong: "Añadir una credencial",
    loadingEntries: "Cargando contenido…",
    catNote: "Nota",
    catCredential: "Credencial",
    catDocument: "Documento",
    back: "← Volver",
    cancel: "← Cancelar",
    edit: "Editar",
    del: "Eliminar",
    delConfirm: "¿Eliminar “{0}”? No se puede deshacer.",
    binaryDoc: "Documento binario ({0} bytes). Gestiona los documentos desde la línea de comandos.",
    fService: "Servicio", fURL: "URL", fUsername: "Usuario", fPassword: "Contraseña", fNotes: "Notas",
    copyBtn: "Copiar", copied: "Copiado", show: "Mostrar", hide: "Ocultar",
    copyFail: "No se pudo copiar — selecciona y copia a mano.",
    newNote: "Nueva nota", editNote: "Editar nota", newCred: "Nueva credencial", editCred: "Editar credencial",
    noteTitle: "Título", noteContent: "Contenido",
    notePh: "Título de la nota", noteContentPh: "Escribe tu nota (admite Markdown)",
    credServicePh: "p. ej. Banco", credNotesPh: "(opcional)",
    create: "Crear", save: "Guardar",
    toastCreated: "Elemento creado.", toastSaved: "Elemento guardado.", toastDeleted: "Elemento eliminado.",
    errTitleRequired: "Hace falta un título.", errServiceRequired: "Hace falta el nombre del servicio.",

    wizSteps: ["Crear caja fuerte", "Guardar secretos", "Interruptor", "Nube (GitHub)", "Paquete", "Listo"],
    wCreateTitle: "Crea tu caja fuerte",
    wCreateSub: "Esta contraseña desbloquea la caja fuerte en este equipo para el día a día. Elige una fuerte que recuerdes — en el siguiente paso recibirás además un código de recuperación y una copia en papel.",
    wConfirmPw: "Confirma la contraseña",
    wFolder: "Carpeta de la caja fuerte",
    wOptional: "(opcional)",
    wFolderPh: "(por defecto) ~/kawarimi-vault",
    wPwPh: "Elige una contraseña fuerte",
    wPw2Ph: "Repite la contraseña",
    wCreateBtn: "Crear caja fuerte",
    errPwShort: "Usa al menos 8 caracteres.",
    errPwMismatch: "Las contraseñas no coinciden.",
    errPwWeak: "Esta contraseña es demasiado débil — elige una más fuerte, o marca la casilla para usarla de todos modos.",
    strength0: "muy débil", strength1: "débil", strength2: "aceptable", strength3: "fuerte", strength4: "excelente",
    strengthLine: "{0} — un atacante con 100k $/año la rompería en {1}",
    wPwWeakChk: "Usar esta contraseña débil de todos modos (no recomendado)",
    durLtHour: "menos de una hora", durHours: "unas {0} horas", durDays: "unos {0} días",
    durMonths: "unos {0} meses", durYears: "unos {0} años", durKYears: "unos {0} miles de años",
    durMYears: "unos {0} millones de años", durBYears: "más de mil millones de años",

    wSecretsTitle: "Apunta esto ahora",
    wSecretsSub: "Se muestran una sola vez. Guárdalos en un lugar seguro y no recargues esta página hasta haberlos guardado.",
    wMnemonic: "Palabras mnemónicas",
    wMnemonicSub: "Tu copia de seguridad personal — guárdala en papel, en un lugar seguro.",
    wRecovery: "Código de recuperación",
    wRecoverySub: "Para recuperar el acceso si pierdes este equipo.",
    wCard: "Frase del destinatario",
    wCardSub: "Imprímela en una tarjeta y dásela a tus destinatarios. La necesitarán — y por sí sola no abre nada.",
    print: "Imprimir",
    savedCheck: "He guardado todo en un lugar seguro",
    wSecretsNext: "Continuar con el interruptor",

    wSwitchTitle: "Interruptor de hombre muerto",
    wSmtpH: "Correo (SMTP)",
    wSmtpSub: "kawarimi te envía los avisos — y, algún día, el correo de entrega a tus destinatarios — a través de tu propia cuenta de correo.",
    wSmtpHelp: "Gmail: tu contraseña normal no funcionará. Crea una “Contraseña de aplicación” en myaccount.google.com/apppasswords y pégala abajo. Sirven los puertos 587 y 465.",
    wSmtpServer: "Servidor SMTP", wSmtpPort: "Puerto", wSmtpUser: "Usuario SMTP", wSmtpPass: "Contraseña SMTP",
    wSmtpPassPh: "contraseña de aplicación",
    wSender: "Correo remitente", wSenderHint: "por defecto, el usuario",
    wRecipH: "Destinatarios y calendario",
    wYourEmail: "Tu correo (para los avisos)",
    wRecipients: "Correos de los destinatarios",
    wRecipientsHint: "uno por línea o separados por comas — quiénes reciben la caja fuerte",
    wTimingHelp: "Si dejas de dar señales de vida: día {0} — recibes un aviso; día {1} — avisos urgentes; día {2} — la clave se envía por correo a tus destinatarios. Cualquier señal de vida vuelve a poner el reloj a cero.",
    wW1: "Aviso 1 (días)", wW2: "Aviso 2 (días)", wFinal: "Entrega final (días)",
    wPkgLoc: "Ubicación del paquete",
    wPkgLocPh: "Enlace desde donde tus destinatarios descargarán el paquete",
    wPkgLocHelp: "Dónde subirás el paquete que se genera en el paso 5 — un enlace compartido de Google Drive / Dropbox, una dirección web… algo que tus destinatarios puedan abrir dentro de años. Este enlace se incluye en el correo de entrega.",
    wOptH: "Canales opcionales",
    wTgToken: "Token del bot de Telegram", wTgChat: "Chat ID de Telegram",
    wImapServer: "Servidor IMAP", wImapPort: "Puerto IMAP",
    wImapPh: "(opcional) dar señales respondiendo a los correos",
    wModeH: "Modo de entrega final",
    wModeCloud: "Solo nube",
    wModeCloudHint: "(recomendado — este equipo no guarda ninguna clave)",
    wModeLocal: "Permitir también la entrega desde este equipo",
    wSaveNext: "Guardar y continuar",
    backBtn: "Atrás",
    errThresholds: "Los días deben ir en aumento: aviso 1 < aviso 2 < entrega final.",

    wCloudTitle: "Armar el interruptor en la nube",
    wCloudSub: "kawarimi crea un repositorio privado de GitHub, configura sus secretos e instala la automatización que enviará el correo a tus destinatarios si dejas de dar señales de vida. Funciona en GitHub aunque tu ordenador esté apagado.",
    wCloudHelp: "Necesitas una cuenta de GitHub y un token: créalo en github.com/settings/tokens/new?scopes=repo (el enlace ya preselecciona el permiso “repo”), elige cualquier caducidad y pégalo abajo. El token se usa una sola vez, solo para configurar, y no se guarda.",
    wCloudTokenLink: "Crear el token (abre GitHub)",
    wGhToken: "Token personal de GitHub",
    wGhRepo: "Nombre del nuevo repositorio privado",
    wCloudBtn: "Crear repositorio y armar el interruptor",
    wCloudWorking: "Trabajando… (puede tardar unos segundos)",

    wPkgTitle: "Generar el paquete para tus destinatarios",
    wPkgSub: "Un zip con la caja fuerte cifrada e instrucciones — sin ningún secreto dentro. Súbelo a la ubicación que indicaste en el paso 3.",
    wPkgAuto: "Incluir los programas",
    wPkgAutoHint: "(compila el programa para cada plataforma; requiere el código fuente)",
    wPkgNone: "Sin programas",
    wPkgNoneHint: "(los destinatarios descargan kawarimi por su cuenta)",
    wPkgOut: "Archivo de salida",
    wPkgOutPh: "(por defecto) ~/kawarimi-vault.zip",
    wPkgBuild: "Generar paquete",
    wPkgBuilding: "Generando…",
    wPkgRebuild: "Volver a generar",
    wPkgBuilt: "✓ Generado {0} ({1} MB)",
    finish: "Terminar",

    wDoneTitle: "Todo listo",
    wDoneSub: "Tu caja fuerte está creada y el interruptor de la nube está armado. Añade contenido y da señales de vida desde el panel. Recuerda entregar a cada destinatario su tarjeta y subir el paquete a su ubicación.",
    goDashboard: "Ir al panel",

    updateAvailable: "Actualización disponible: v{0}",
    updateNow: "Actualizar ahora",
    whatsNew: "Novedades",
    updating: "Actualizando…",
    updatedRestart: "Actualizado a v{0}. Cierra kawarimi y ábrelo de nuevo para usar la nueva versión.",
    updateFailed: "La actualización falló: {0}",
  },
};

let lang = (function () {
  const saved = localStorage.getItem("kawarimi-lang");
  if (saved === "es" || saved === "en") return saved;
  return (navigator.language || "").toLowerCase().startsWith("es") ? "es" : "en";
})();

function t(key) { return (I18N[lang] && I18N[lang][key]) ?? I18N.en[key] ?? key; }
function fmt(s, ...args) { return s.replace(/\{(\d)\}/g, (_, i) => String(args[i])); }

function applyLang() {
  document.documentElement.lang = lang;
  const btn = document.getElementById("langBtn");
  if (btn) { btn.textContent = lang.toUpperCase(); btn.title = t("langTitle"); }
  const sub = document.querySelector(".brand-sub");
  if (sub) sub.textContent = t("ownerConsole");
  const quit = document.getElementById("quitBtn");
  if (quit) quit.textContent = t("quit");
  const theme = document.getElementById("themeBtn");
  if (theme) theme.title = t("themeTitle");
}

function initLang() {
  applyLang();
  document.getElementById("langBtn").addEventListener("click", () => {
    lang = lang === "es" ? "en" : "es";
    localStorage.setItem("kawarimi-lang", lang);
    applyLang();
    if (state) render();
  });
}

// help() renders an inline guidance line; URLs inside the text become links.
function help(text) {
  const parts = text.split(/(\S+\.(?:com|org)\S*)/g);
  return h("p", { class: "help" }, parts.map((p) =>
    /\.(com|org)/.test(p) && !p.includes(" ")
      ? h("a", { href: "https://" + p.replace(/^https?:\/\//, ""), target: "_blank", rel: "noreferrer" }, p)
      : p));
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
  updateStateDot();
  if (!state.configured) return viewWizard();
  if (!state.unlocked) return viewUnlock();
  if (nav === "wizard") return viewWizard(); // resumed switch/cloud setup for an existing vault
  if (nav === "entries") return viewEntries();
  return viewDashboard();
}

// appShell wraps unlocked-state content with the top navigation.
function appShell(active, ...content) {
  const tab = (id, label) => h("button", {
    class: "navtab" + (active === id ? " active" : ""), type: "button",
    onclick: () => { nav = id; render(); }
  }, label);
  return h("div", null,
    h("div", { class: "navbar" }, tab("dashboard", t("navDashboard")), tab("entries", t("navEntries"))),
    ...content);
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

// ---- unlock -------------------------------------------------------------------

function viewUnlock() {
  const err = h("div", { class: "error" });
  const pw = h("input", { type: "password", id: "pw", placeholder: t("vaultPassword"), autofocus: "true" });
  const btn = h("button", { class: "btn", type: "submit" }, t("unlockBtn"));

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
    h("h1", null, t("unlockTitle")),
    h("p", { class: "sub" }, t("unlockSub")),
    h("label", null, t("password")),
    pw, err,
    h("div", { class: "btn-row" }, btn)
  );

  setView(h("div", { class: "card card-narrow" }, form));
  pw.focus();
}

// ---- dashboard: the switch panel ------------------------------------------------

// switchStatus derives the state word + tone the whole page keys off.
function switchStatus() {
  if (!state.switchConfigured) return { word: t("stNotArmed"), tone: "off", note: t("noteNotSetup") };
  if (state.daysSince < 0) return { word: t("stArmed"), tone: "warn", note: t("noteNoCheckin") };
  const d = state.daysSince, w1 = state.warning1Days, w2 = state.warning2Days, f = state.finalDays;
  if (f > 0 && d >= f) return { word: t("stReleasing"), tone: "alarm", note: t("notePastRelease") };
  if (w2 > 0 && d >= w2) return { word: t("stOverdue"), tone: "alarm", note: fmt(t("noteOverdueUrgent"), f) };
  if (w1 > 0 && d >= w1) return { word: t("stOverdue"), tone: "warn", note: fmt(t("noteOverdueWarn"), f) };
  return { word: t("stQuiet"), tone: "quiet", note: fmt(t(d === 1 ? "noteQuiet1" : "noteQuietN"), d) };
}

function updateStateDot() {
  const dot = document.getElementById("stateDot");
  if (!dot) return;
  const tone = (state && state.configured && state.unlocked) ? switchStatus().tone : "off";
  dot.className = "brand-dot" + (tone === "quiet" ? " armed" : tone === "warn" ? " warn" : tone === "alarm" ? " alarm" : "");
}

// timelineNode renders the check-in schedule: a track spanning day 0 → release,
// zoned quiet / warning / urgent, with a marker at daysSince. Structure carries the
// switch's real schedule.
function timelineNode() {
  const f = state.finalDays, w1 = state.warning1Days, w2 = state.warning2Days;
  if (!(f > 0 && w1 > 0 && w2 > w1 && f > w2)) return null;
  const d = Math.max(state.daysSince, 0);
  const pct = (n) => Math.min(100, (n / f) * 100);
  const markerPct = Math.min(99, Math.max(1, pct(d))); // keep the dot inside the track ends
  const tone = switchStatus().tone;

  const track = h("div", { class: "tl-track" },
    h("div", { class: "tl-zones" },
      h("div", { class: "tl-zone z-quiet", style: "width:" + pct(w1) + "%" }),
      h("div", { class: "tl-zone z-warn", style: "width:" + (pct(w2) - pct(w1)) + "%" }),
      h("div", { class: "tl-zone z-urgent", style: "width:" + (100 - pct(w2)) + "%" }),
      h("div", { class: "tl-cap" })),
    h("div", { class: "tl-marker " + tone, style: "left:" + markerPct + "%" }));

  const ticks = h("div", { class: "tl-ticks" },
    h("div", { class: "tl-tick start", style: "left:0" }, t("tickDay0")),
    h("div", { class: "tl-tick", style: "left:" + pct(w1) + "%" }, fmt(t("tickWarn1"), w1)),
    h("div", { class: "tl-tick", style: "left:" + pct(w2) + "%" }, fmt(t("tickWarn2"), w2)),
    h("div", { class: "tl-tick end", style: "left:100%" }, fmt(t("tickRelease"), f)));

  return h("div", { class: "timeline" }, track, ticks);
}

function viewDashboard() {
  updateStateDot();
  const st = switchStatus();

  const facts = h("div", { class: "facts" },
    fact(t("factEntries"), String(state.entryCount)),
    fact(t("factLastCheckin"), state.lastCheckin || t("never")),
    state.switchConfigured ? fact(t("factReleaseMode"), state.cloudOnly ? t("modeCloudOnly") : t("modeCloudPlus")) : null,
    fact(t("factVault"), state.vaultDir || "—", true));

  setView(appShell("dashboard",
    updateBanner(),
    h("div", { class: "card" },
      h("p", { class: "eyebrow" }, t("eyebrowStatus")),
      h("div", { class: "switch-state" },
        h("span", { class: "switch-word " + st.tone }, st.word),
        h("span", { class: "switch-note" }, st.note)),
      timelineNode(),
      h("div", { class: "btn-row" },
        state.switchConfigured
          ? h("button", { class: "btn", type: "button", onclick: doCheckin }, t("checkinBtn"))
          : h("button", { class: "btn", type: "button", onclick: startSwitchSetup }, t("setupSwitchBtn")),
        h("span", { class: "spacer" }),
        state.switchConfigured
          ? h("button", { class: "btn btn-ghost", type: "button", onclick: doVerify }, t("verifyBtn"))
          : null),
      facts)
  ));

  // First time we land on the dashboard, check for an update and re-render if one
  // is found (keeps the initial paint instant).
  if (updateInfo === null) {
    checkForUpdate().then(() => { if (nav === "dashboard") viewDashboard(); });
  }
}

let updateInfo = null; // {available, version, url}; null = not checked yet this session

async function checkForUpdate() {
  try { updateInfo = await api("/api/update/check"); }
  catch (_) { updateInfo = { available: false }; }
  return updateInfo;
}

function updateBanner() {
  if (!updateInfo || !updateInfo.available) return null;
  return h("div", { class: "update-banner" },
    h("span", { class: "ub-dot" }),
    h("span", { class: "ub-text" }, fmt(t("updateAvailable"), updateInfo.version)),
    updateInfo.url ? h("a", { class: "ub-link", href: updateInfo.url, target: "_blank", rel: "noreferrer" }, t("whatsNew")) : null,
    h("span", { class: "spacer" }),
    h("button", { class: "btn btn-sm", type: "button", onclick: doUpdate }, t("updateNow")));
}

async function doUpdate(e) {
  const btn = e.target; btn.disabled = true; btn.textContent = t("updating");
  try {
    const r = await api("/api/update/apply", { method: "POST" });
    if (r.restart) toast(fmt(t("updatedRestart"), r.version));
    else { updateInfo = { available: false }; if (nav === "dashboard") viewDashboard(); }
  } catch (ex) {
    toast(fmt(t("updateFailed"), ex.message), true);
    btn.disabled = false; btn.textContent = t("updateNow");
  }
}

function fact(k, v, wide) {
  if (v == null) return null;
  return h("div", { class: "fact" + (wide ? " wide" : "") }, h("div", { class: "k" }, k), h("div", { class: "v" }, v));
}

async function doCheckin(e) {
  const btn = e.target; btn.disabled = true;
  try {
    const r = await api("/api/checkin", { method: "POST" });
    if (r.pushed) {
      toast(t("toastCheckinCloud"));
    } else if (r.cloudError) {
      toast(fmt(t("toastCheckinCloudFail"), r.cloudError), true);
    } else {
      toast(t("toastCheckinLocal"));
    }
    await refresh();
  } catch (ex) {
    toast(ex.message, true); btn.disabled = false;
  }
}

async function doVerify(e) {
  const btn = e.target; btn.disabled = true;
  try {
    const r = await api("/api/switch/verify", { method: "POST" });
    toast(r.ok ? t("toastVerifyOk") : t("toastVerifyWarn"), !r.ok);
  } catch (ex) {
    toast(ex.message, true);
  } finally { btn.disabled = false; }
}

// ---- entries ------------------------------------------------------------------

function catLabel(c) {
  return c === "notes" ? t("catNote") : c === "credentials" ? t("catCredential") : c === "documents" ? t("catDocument") : c;
}

function viewEntries() {
  setView(appShell("entries", h("div", { class: "loading" }, t("loadingEntries"))));
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
    ? h("div", { class: "empty-state" },
      h("p", null, t("emptyVault")),
      h("div", null,
        h("button", { class: "btn", type: "button", onclick: () => renderEntryEditor("notes", null) }, t("writeNote")),
        " ",
        h("button", { class: "btn btn-ghost", type: "button", onclick: () => renderEntryEditor("credentials", null) }, t("addCredentialLong"))))
    : h("div", { class: "entry-list" },
      entries.map((e) => h("button", { class: "entry-row", type: "button", onclick: () => openEntry(e.id) },
        h("span", { class: "entry-cat" }, catLabel(e.category)),
        h("span", { class: "entry-title" }, e.title || e.name),
        h("span", { class: "entry-date" }, (e.updatedAt || "").slice(0, 10)))));

  setView(appShell("entries",
    h("div", { class: "card" },
      h("div", { class: "list-head" },
        h("h1", null, t("entriesTitle")),
        h("div", { class: "btn-row-inline" },
          h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: () => renderEntryEditor("notes", null) }, t("addNote")),
          h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: () => renderEntryEditor("credentials", null) }, t("addCredential")))),
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
      credRow(t("fService"), c.service), credRow(t("fURL"), c.url),
      credRow(t("fUsername"), c.username), credRow(t("fPassword"), c.password, true),
      credRow(t("fNotes"), c.notes));
  } else {
    bodyNode = h("p", { class: "muted" }, fmt(t("binaryDoc"), e.size || 0));
  }

  const canEdit = e.category === "notes" || e.category === "credentials";
  setView(appShell("entries",
    h("div", { class: "card" },
      h("div", { class: "list-head" },
        h("h1", null, e.title || e.name),
        h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: () => loadEntries() }, t("back"))),
      h("div", { class: "muted" }, catLabel(e.category)),
      bodyNode,
      h("div", { class: "btn-row" },
        canEdit ? h("button", { class: "btn", type: "button", onclick: () => renderEntryEditor(e.category, e) }, t("edit")) : null,
        h("span", { class: "spacer" }),
        h("button", { class: "btn btn-danger", type: "button", onclick: () => deleteEntry(e) }, t("del"))))));
}

function credRow(label, value, secret) {
  if (!value) return null;
  const val = h("span", { class: "mono" + (secret ? " secret-mask" : "") }, value);
  return h("div", { class: "cred-row" },
    h("span", { class: "cred-k" }, label),
    val,
    h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: (ev) => copy(value, ev.target) }, t("copyBtn")),
    secret ? h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: (ev) => { val.classList.toggle("secret-mask"); ev.target.textContent = val.classList.contains("secret-mask") ? t("show") : t("hide"); } }, t("show")) : null);
}

function renderEntryEditor(category, entry) {
  const isNew = !entry;
  const err = h("div", { class: "error" });
  const fields = {};
  let form;

  if (category === "notes") {
    const title = h("input", { type: "text", id: "ntitle", value: entry ? (entry.title || "") : "", placeholder: t("notePh") });
    const content = h("textarea", { id: "ncontent", placeholder: t("noteContentPh") });
    content.value = entry ? (entry.content || "") : "";
    fields.get = () => {
      if (isNew && !title.value.trim()) throw new Error(t("errTitleRequired"));
      return { category: "notes", title: title.value, content: content.value };
    };
    form = [h("label", null, t("noteTitle")), title, h("label", null, t("noteContent")), content];
    if (!isNew) title.disabled = true; // title is fixed after creation (filename-derived)
  } else {
    const c = (entry && entry.credential) || {};
    const mk = (id, ph, val, type) => (fields[id] = h("input", { id, type: type || "text", placeholder: ph, value: val || "" }));
    form = [
      h("label", null, t("fService")), mk("service", t("credServicePh"), c.service),
      h("label", null, t("fURL")), mk("url", "https://…", c.url),
      h("label", null, t("fUsername")), mk("username", "user@example.com", c.username),
      h("label", null, t("fPassword")), mk("password", "", c.password, "text"),
      h("label", null, t("fNotes")), (fields.notes = h("textarea", { id: "cnotes", placeholder: t("credNotesPh") })),
    ];
    fields.notes.value = c.notes || "";
    if (!isNew) fields.service.disabled = true;
    fields.get = () => {
      if (isNew && !fields.service.value.trim()) throw new Error(t("errServiceRequired"));
      return {
        category: "credentials",
        credential: { service: fields.service.value, url: fields.url.value, username: fields.username.value, password: fields.password.value, notes: fields.notes.value }
      };
    };
  }

  const btn = h("button", { class: "btn", type: "submit" }, isNew ? t("create") : t("save"));
  const el = h("form", {
    onsubmit: async (e) => {
      e.preventDefault(); err.textContent = ""; btn.disabled = true;
      try {
        const body = fields.get();
        if (isNew) await api("/api/entries", { method: "POST", body });
        else await api("/api/entries/" + encodeURIComponent(entry.id), { method: "PUT", body });
        toast(isNew ? t("toastCreated") : t("toastSaved"));
        loadEntries();
      } catch (ex) { err.textContent = ex.message; btn.disabled = false; }
    }
  },
    h("div", { class: "list-head" },
      h("h1", null, category === "notes" ? (isNew ? t("newNote") : t("editNote")) : (isNew ? t("newCred") : t("editCred"))),
      h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: () => loadEntries() }, t("cancel"))),
    ...form, err,
    h("div", { class: "btn-row" }, btn));

  setView(appShell("entries", h("div", { class: "card" }, el)));
}

async function deleteEntry(e) {
  if (!confirm(fmt(t("delConfirm"), e.title || e.name))) return;
  try {
    await api("/api/entries/" + encodeURIComponent(e.id), { method: "DELETE" });
    toast(t("toastDeleted"));
    loadEntries();
  } catch (ex) { toast(ex.message, true); }
}

// ---- setup wizard -------------------------------------------------------------

let wiz = { step: 0, secrets: null };

function viewWizard() {
  if (wiz.step === 1 && !wiz.secrets) wiz.step = 0; // lost secrets after reload
  const steps = [wizCreate, wizSecrets, wizSwitch, wizCloud, wizPackage, wizDone];
  setView(h("div", null, wizProgress(), steps[wiz.step]()));
}

function wizProgress() {
  return h("div", { class: "wizard-steps" },
    t("wizSteps").map((label, i) =>
      h("div", { class: "wstep" + (i === wiz.step ? " active" : "") + (i < wiz.step ? " done" : "") },
        h("span", { class: "wstep-num" }, i < wiz.step ? "✓" : String(i + 1)),
        h("span", { class: "wstep-label" }, label))));
}

function wizGoto(step) { wiz.step = step; viewWizard(); }

// ---- password strength meter ----------------------------------------------------

// fmtCrackYears renders an expected crack time (in years) as a rough localized
// duration, mirroring crypto.FormatCrackTime.
function fmtCrackYears(y) {
  if (y < 1 / 8766) return t("durLtHour");
  if (y < 2 / 365) return fmt(t("durHours"), Math.round(y * 8766));
  if (y < 2 / 12) return fmt(t("durDays"), Math.round(y * 365.25));
  if (y < 2) return fmt(t("durMonths"), Math.round(y * 12));
  if (y < 1000) return fmt(t("durYears"), Math.round(y));
  if (y < 1e6) return fmt(t("durKYears"), Math.round(y / 1e3));
  if (y < 1e9) return fmt(t("durMYears"), Math.round(y / 1e6));
  return t("durBYears");
}

// strengthMeter builds a live meter under a new-password input. Scores come from
// the backend estimator (one source of truth with the CLI); meter.level holds the
// latest level (null while empty) and meter.onscore fires after each update.
function strengthMeter(pwInput) {
  const fill = h("div", { class: "smeter-fill" });
  const label = h("div", { class: "smeter-label" });
  const box = h("div", { class: "smeter" }, h("div", { class: "smeter-track" }, fill), label);
  box.level = null;
  let timer = null;
  pwInput.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      if (!pwInput.value) {
        box.level = null;
        fill.style.width = "0";
        label.textContent = "";
        if (box.onscore) box.onscore();
        return;
      }
      try {
        const s = await api("/api/password-strength", { method: "POST", body: { password: pwInput.value } });
        if (!pwInput.value) return; // field cleared while the request was in flight
        box.level = s.level;
        fill.style.width = ((s.level + 1) * 20) + "%";
        fill.dataset.level = String(s.level);
        label.textContent = fmt(t("strengthLine"), t("strength" + s.level), fmtCrackYears(s.crackYears));
        if (box.onscore) box.onscore();
      } catch (_) { /* the meter is advisory — never block typing on an error */ }
    }, 150);
  });
  return box;
}

// Step 1: create the vault.
function wizCreate() {
  const err = h("div", { class: "error" });
  const pw = h("input", { type: "password", id: "wpw", placeholder: t("wPwPh") });
  const pw2 = h("input", { type: "password", id: "wpw2", placeholder: t("wPw2Ph") });
  const dir = h("input", { type: "text", id: "wdir", placeholder: t("wFolderPh") });
  const btn = h("button", { class: "btn", type: "submit" }, t("wCreateBtn"));
  const meter = strengthMeter(pw);
  const weakChk = h("input", { type: "checkbox", id: "wweak" });
  const weakRow = h("label", { class: "weak-row" }, weakChk, " ", t("wPwWeakChk"));
  weakRow.style.display = "none";
  const isWeak = () => meter.level !== null && meter.level < 2;
  meter.onscore = () => {
    weakRow.style.display = isWeak() ? "" : "none";
    if (!isWeak()) weakChk.checked = false;
  };

  const form = h("form", {
    onsubmit: async (e) => {
      e.preventDefault();
      err.textContent = "";
      if (pw.value.length < 8) { err.textContent = t("errPwShort"); return; }
      if (pw.value !== pw2.value) { err.textContent = t("errPwMismatch"); return; }
      if (isWeak() && !weakChk.checked) { err.textContent = t("errPwWeak"); return; }
      btn.disabled = true;
      try {
        wiz.secrets = await api("/api/init", {
          method: "POST",
          body: { password: pw.value, vaultDir: dir.value.trim(), acceptWeak: weakChk.checked }
        });
        wizGoto(1);
      } catch (ex) {
        // The server enforces the same gate; if the meter had not scored yet,
        // surface the override instead of a raw error key.
        if (ex.message === "weak_password") { err.textContent = t("errPwWeak"); weakRow.style.display = ""; }
        else err.textContent = ex.message;
        btn.disabled = false;
      }
    }
  },
    h("h1", null, t("wCreateTitle")),
    h("p", { class: "sub" }, t("wCreateSub")),
    h("label", null, t("password")), pw, meter,
    h("label", null, t("wConfirmPw")), pw2,
    h("label", null, t("wFolder"), " ", h("span", { class: "hint" }, t("wOptional"))), dir,
    weakRow,
    err,
    h("div", { class: "btn-row" }, btn)
  );
  return h("div", { class: "card card-narrow" }, form);
}

// Step 2: one-time secrets.
function wizSecrets() {
  const s = wiz.secrets;
  const saved = h("input", { type: "checkbox", id: "savedChk" });
  const cont = h("button", { class: "btn", type: "button", disabled: "true" }, t("wSecretsNext"));
  saved.addEventListener("change", () => { cont.disabled = !saved.checked; });
  cont.addEventListener("click", () => wizGoto(2));

  return h("div", { class: "card" },
    h("h1", null, t("wSecretsTitle")),
    h("p", { class: "sub" }, t("wSecretsSub")),
    secretBlock(t("wMnemonic"), t("wMnemonicSub"),
      h("div", { class: "word-grid" },
        s.mnemonic.map((wd, i) => h("div", { class: "word" }, h("span", { class: "word-i" }, String(i + 1)), wd))),
      s.mnemonic.join(" ")),
    secretBlock(t("wRecovery"), t("wRecoverySub"),
      h("div", { class: "secret-val mono" }, s.recoveryCode), s.recoveryCode),
    secretBlock(t("wCard"), t("wCardSub"),
      h("div", { class: "secret-val mono" }, s.recipientPassphrase), s.recipientPassphrase),
    h("div", { class: "btn-row" },
      h("button", { class: "btn btn-ghost", type: "button", onclick: () => window.print() }, t("print")),
      h("span", { class: "spacer" }),
      h("label", { class: "inline-check" }, saved, " " + t("savedCheck"))),
    h("div", { class: "btn-row" }, cont)
  );
}

function secretBlock(title, sub, valNode, copyText) {
  return h("div", { class: "secret-box" },
    h("div", { class: "secret-head" },
      h("div", null, h("div", { class: "secret-title" }, title), h("div", { class: "muted" }, sub)),
      h("button", { class: "btn btn-ghost btn-sm", type: "button", onclick: (e) => copy(copyText, e.target) }, t("copyBtn"))),
    valNode);
}

function copy(text, btn) {
  navigator.clipboard.writeText(text).then(() => {
    const old = btn.textContent; btn.textContent = t("copied"); setTimeout(() => { btn.textContent = old; }, 1200);
  }).catch(() => toast(t("copyFail"), true));
}

// Step 3: dead man's switch settings.
function wizSwitch() {
  const err = h("div", { class: "error" });
  const f = {};
  const inp = (id, attrs) => (f[id] = h("input", Object.assign({ id }, attrs)));
  const btn = h("button", { class: "btn", type: "submit" }, t("wSaveNext"));

  const form = h("form", {
    onsubmit: async (e) => {
      e.preventDefault();
      err.textContent = "";
      const w1 = parseInt(f.w1.value) || 0, w2 = parseInt(f.w2.value) || 0, fin = parseInt(f.final.value) || 0;
      if (!(w1 > 0 && w1 < w2 && w2 < fin)) { err.textContent = t("errThresholds"); return; }
      btn.disabled = true;
      const body = {
        smtpServer: f.smtpServer.value, smtpPort: parseInt(f.smtpPort.value) || 587,
        smtpUsername: f.smtpUsername.value, smtpPassword: f.smtpPassword.value,
        senderEmail: f.senderEmail.value, userEmail: f.userEmail.value,
        recipients: f.recipients.value.split(/[,\n]/).map((x) => x.trim()).filter(Boolean),
        warning1Days: w1, warning2Days: w2, finalDays: fin,
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
    h("h2", null, t("wSmtpH")),
    h("p", { class: "muted" }, t("wSmtpSub")),
    help(t("wSmtpHelp")),
    h("div", { class: "row" },
      field(t("wSmtpServer"), inp("smtpServer", { type: "text", placeholder: "smtp.gmail.com" })),
      field(t("wSmtpPort"), inp("smtpPort", { type: "number", value: "587" }))),
    h("div", { class: "row" },
      field(t("wSmtpUser"), inp("smtpUsername", { type: "text", placeholder: "you@gmail.com" })),
      field(t("wSmtpPass"), inp("smtpPassword", { type: "password", placeholder: t("wSmtpPassPh") }))),
    fieldNode(t("wSender"), t("wSenderHint"), inp("senderEmail", { type: "text" })),

    h("h2", null, t("wRecipH")),
    field(t("wYourEmail"), inp("userEmail", { type: "email", placeholder: "you@example.com" })),
    fieldNode(t("wRecipients"), t("wRecipientsHint"),
      (f.recipients = h("textarea", { id: "recipients", placeholder: "family@example.com" }))),
    help(fmt(t("wTimingHelp"), 14, 21, 30)),
    h("div", { class: "row" },
      field(t("wW1"), inp("w1", { type: "number", value: "14" })),
      field(t("wW2"), inp("w2", { type: "number", value: "21" })),
      field(t("wFinal"), inp("final", { type: "number", value: "30" }))),
    field(t("wPkgLoc"), inp("pkgLoc", { type: "text", placeholder: t("wPkgLocPh") })),
    help(t("wPkgLocHelp")),

    h("h2", null, t("wOptH")),
    h("div", { class: "row" },
      field(t("wTgToken"), inp("tgToken", { type: "text", placeholder: t("wOptional") })),
      field(t("wTgChat"), inp("tgChat", { type: "text", placeholder: t("wOptional") }))),
    h("div", { class: "row" },
      field(t("wImapServer"), inp("imapServer", { type: "text", placeholder: t("wImapPh") })),
      field(t("wImapPort"), inp("imapPort", { type: "number", placeholder: "993" }))),

    h("h2", null, t("wModeH")),
    h("label", { class: "inline-check" },
      h("input", { type: "radio", name: "release", value: "cloud", checked: "true" }),
      " " + t("wModeCloud") + " ", h("span", { class: "hint" }, t("wModeCloudHint"))),
    h("label", { class: "inline-check" },
      h("input", { type: "radio", name: "release", value: "local" }),
      " " + t("wModeLocal")),
    err,
    h("div", { class: "btn-row" },
      h("button", { class: "btn btn-ghost", type: "button", onclick: () => wizResume ? exitWizardToDashboard() : wizGoto(1) }, wizResume ? t("cancel") : t("backBtn")),
      h("span", { class: "spacer" }), btn)
  );
  return h("div", { class: "card" }, h("h1", null, t("wSwitchTitle")), form);
}

// Step 4: GitHub cloud automation.
function wizCloud() {
  const err = h("div", { class: "error" });
  const tok = h("input", { type: "password", id: "ghtok", placeholder: "ghp_…" });
  const repo = h("input", { type: "text", id: "ghrepo", value: "kawarimi-dms" });
  const btn = h("button", { class: "btn", type: "submit" }, t("wCloudBtn"));

  const form = h("form", {
    onsubmit: async (e) => {
      e.preventDefault();
      err.textContent = "";
      btn.disabled = true; btn.textContent = t("wCloudWorking");
      try {
        const r = await api("/api/switch/cloud", { method: "POST", body: { githubToken: tok.value, repoName: repo.value.trim() } });
        wiz.cloud = r;
        wizGoto(4);
      } catch (ex) { err.textContent = ex.message; btn.disabled = false; btn.textContent = t("wCloudBtn"); }
    }
  },
    h("h1", null, t("wCloudTitle")),
    h("p", { class: "sub" }, t("wCloudSub")),
    help(t("wCloudHelp")),
    h("p", null, h("a", { class: "help-link", href: "https://github.com/settings/tokens/new?scopes=repo&description=kawarimi-dms", target: "_blank", rel: "noreferrer" }, t("wCloudTokenLink"))),
    h("label", null, t("wGhToken")), tok,
    h("label", null, t("wGhRepo")), repo,
    err,
    h("div", { class: "btn-row" },
      h("button", { class: "btn btn-ghost", type: "button", onclick: () => wizGoto(2) }, t("backBtn")),
      h("span", { class: "spacer" }), btn)
  );
  return h("div", { class: "card card-narrow" }, form);
}

// Step 5: build the recipient package.
function wizPackage() {
  const err = h("div", { class: "error" });
  const out = h("input", { type: "text", id: "pkgout", placeholder: t("wPkgOutPh") });
  const result = h("div", { class: "muted" });
  const btn = h("button", { class: "btn", type: "submit" }, t("wPkgBuild"));

  const form = h("form", {
    onsubmit: async (e) => {
      e.preventDefault();
      err.textContent = ""; result.textContent = "";
      btn.disabled = true; btn.textContent = t("wPkgBuilding");
      const mode = document.querySelector('input[name="pkgmode"]:checked').value;
      try {
        const r = await api("/api/package/build", { method: "POST", body: { mode, output: out.value.trim() } });
        result.innerHTML = "";
        result.appendChild(h("div", { class: "ok-line" }, fmt(t("wPkgBuilt"), r.path, r.sizeMB)));
        btn.textContent = t("wPkgRebuild");
        btn.disabled = false;
      } catch (ex) { err.textContent = ex.message; btn.disabled = false; btn.textContent = t("wPkgBuild"); }
    }
  },
    h("h1", null, t("wPkgTitle")),
    h("p", { class: "sub" }, t("wPkgSub")),
    h("label", { class: "inline-check" },
      h("input", { type: "radio", name: "pkgmode", value: "auto", checked: "true" }),
      " " + t("wPkgAuto") + " ", h("span", { class: "hint" }, t("wPkgAutoHint"))),
    h("label", { class: "inline-check" },
      h("input", { type: "radio", name: "pkgmode", value: "none" }),
      " " + t("wPkgNone") + " ", h("span", { class: "hint" }, t("wPkgNoneHint"))),
    h("label", null, t("wPkgOut"), " ", h("span", { class: "hint" }, t("wOptional"))), out,
    err, result,
    h("div", { class: "btn-row" },
      h("button", { class: "btn btn-ghost", type: "button", onclick: () => wizGoto(3) }, t("backBtn")),
      h("span", { class: "spacer" }),
      btn,
      h("button", { class: "btn", type: "button", onclick: () => wizGoto(5) }, t("finish"))))
  ;
  return h("div", { class: "card" }, form);
}

// Step 6: done.
function wizDone() {
  return h("div", { class: "card card-narrow" },
    h("h1", null, t("wDoneTitle")),
    h("p", { class: "sub" }, t("wDoneSub")),
    h("div", { class: "btn-row" },
      h("button", { class: "btn", type: "button", onclick: exitWizardToDashboard }, t("goDashboard")))
  );
}

// small field helpers
function field(label, node) { return fieldNode(label, null, node); }
function fieldNode(label, hint, node) {
  return h("div", null, h("label", null, label, hint ? h("span", { class: "hint" }, "  " + hint) : null), node);
}

// ---- theme ----------------------------------------------------------------------

// Theme: "auto" (follow the OS), "dark", or "light". Persisted in localStorage;
// a #theme=dark|light hash pins it for this load (used by screenshot tooling too).
const THEMES = ["auto", "dark", "light"];
const THEME_GLYPH = { auto: "◐", dark: "●", light: "○" };

function applyTheme(mode) {
  if (mode === "dark" || mode === "light") {
    document.documentElement.setAttribute("data-theme", mode);
  } else {
    document.documentElement.removeAttribute("data-theme");
  }
  const btn = document.getElementById("themeBtn");
  if (btn) {
    btn.textContent = THEME_GLYPH[mode] || THEME_GLYPH.auto;
    btn.title = t("themeTitle") + ": " + mode;
  }
}

function currentTheme() {
  const m = (location.hash.match(/theme=(dark|light)/) || [])[1];
  if (m) return m;
  const saved = localStorage.getItem("kawarimi-theme");
  return THEMES.includes(saved) ? saved : "auto";
}

function initTheme() {
  applyTheme(currentTheme());
  document.getElementById("themeBtn").addEventListener("click", () => {
    const next = THEMES[(THEMES.indexOf(currentTheme()) + 1) % THEMES.length];
    localStorage.setItem("kawarimi-theme", next);
    if (location.hash.includes("theme=")) history.replaceState(null, "", location.pathname);
    applyTheme(next);
  });
}

// ---- lifecycle ----------------------------------------------------------------

function startKeepalive() {
  setInterval(() => { api("/api/ping").catch(() => {}); }, 30000);
}

document.getElementById("quitBtn").addEventListener("click", async () => {
  if (!confirm(t("quitConfirm"))) return;
  try { await api("/api/quit", { method: "POST" }); } catch (_) {}
  document.body.innerHTML =
    '<div style="padding:60px;text-align:center;color:#888">' + t("quitDone") + "</div>";
});

(async function main() {
  // A #lang=es|en hash pins the language for this load (screenshots, links).
  const lm = (location.hash.match(/lang=(es|en)/) || [])[1];
  if (lm) lang = lm;
  initTheme();
  initLang();
  if (location.hash.includes("entries")) nav = "entries"; // deep-link the Entries tab
  startKeepalive();
  try {
    await refresh();
  } catch (ex) {
    setView(h("div", { class: "card" }, h("h1", null, t("cannotReach")), h("p", { class: "error" }, ex.message)));
  }
})();
