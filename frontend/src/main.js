import "./style.css";
import {
  Bootstrap,
  CompleteOnboarding,
  OpenWorkspace,
  RefreshProviders,
} from "../wailsjs/go/main/App";
import claudeLogo from "./assets/icons/brand/claude.svg";
import codexLogo from "./assets/icons/brand/codex.svg";

const root = document.querySelector("#app");

let state = null;
let selectedWorkspaceID = null;
let busy = false;
let noticeTimer = null;

const providerLogos = {
  claude: claudeLogo,
  codex: codexLogo,
};

const iconPaths = {
  brand: '<path d="m7 4 5 8-5 8M17 4l-5 8 5 8"></path>',
  search: '<circle cx="11" cy="11" r="7"></circle><path d="m20 20-4-4"></path>',
  plus: '<path d="M12 5v14M5 12h14"></path>',
  laptop: '<rect x="4" y="5" width="16" height="11" rx="2"></rect><path d="M2.5 19h19"></path>',
  folder: '<path d="M3 7.5A2.5 2.5 0 0 1 5.5 5H10l2 2h6.5A2.5 2.5 0 0 1 21 9.5v7A2.5 2.5 0 0 1 18.5 19h-13A2.5 2.5 0 0 1 3 16.5v-9Z"></path>',
  refresh: '<path d="M20 7v5h-5"></path><path d="M18.1 16a8 8 0 1 1 .3-8.3L20 12"></path>',
  arrow: '<path d="M5 12h14M14 7l5 5-5 5"></path>',
  close: '<path d="m7 7 10 10M17 7 7 17"></path>',
  terminal: '<rect x="3" y="4" width="18" height="16" rx="2"></rect><path d="m7 9 3 3-3 3M13 15h4"></path>',
  check: '<path d="m5 12 4 4L19 6"></path>',
  chevron: '<path d="m9 18 6-6-6-6"></path>',
  more: '<circle cx="5" cy="12" r="1"></circle><circle cx="12" cy="12" r="1"></circle><circle cx="19" cy="12" r="1"></circle>',
};

function icon(name, className = "") {
  return `<svg class="ui-icon ${escapeHTML(className)}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${iconPaths[name] || iconPaths.terminal}</svg>`;
}

function providerIcon(provider, className = "") {
  const source = providerLogos[provider?.id];
  if (!source) return icon("terminal", className);
  return `<img class="provider-logo ${escapeHTML(className)}" src="${source}" alt="" />`;
}

const escapeHTML = (value = "") =>
  String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");

function platformName(value) {
  if (value === "darwin") return "macOS";
  if (value === "windows") return "Windows";
  if (value === "linux") return "Linux";
  return value || "Unknown OS";
}

function selectedProviders() {
  const selected = new Set(state?.selectedProviderIds || []);
  return (state?.providers || []).filter(
    (provider) => selected.has(provider.id) && provider.installed && provider.supported,
  );
}

function providerByID(id) {
  return (state?.providers || []).find((provider) => provider.id === id);
}

function selectedWorkspace() {
  const workspaces = state?.workspaces || [];
  return workspaces.find((workspace) => workspace.id === selectedWorkspaceID) || workspaces[0] || null;
}

function renderProviderRows(providers, selectable = false) {
  return providers
    .map((provider) => {
      const available = provider.installed && provider.supported && !provider.comingSoon;
      const checked = available ? "checked" : "";
      const disabled = available ? "" : "disabled";
      const status = provider.comingSoon
        ? "Coming soon"
        : provider.installed
          ? provider.version || "Installed"
          : "Not found";

      return `
        <label class="provider-option ${available ? "is-available" : "is-disabled"}">
          <span class="provider-option__mark">${providerIcon(provider)}</span>
          <span class="provider-option__copy">
            <strong>${escapeHTML(provider.name)}</strong>
            <small>${escapeHTML(provider.description)}</small>
          </span>
          <span class="provider-option__status">${escapeHTML(status)}</span>
          ${selectable ? `<input type="checkbox" name="provider" value="${escapeHTML(provider.id)}" ${checked} ${disabled} />` : ""}
        </label>`;
    })
    .join("");
}

function renderOnboarding() {
  const device = state.device;
  root.innerHTML = `
    <main class="onboarding-shell">
      <section class="onboarding-intro">
        <div class="app-brand"><span class="brand-mark">${icon("brand")}</span><strong>Agent X</strong></div>
        <p class="eyebrow">First-time setup</p>
        <h1>Make this device<br />part of your workspace.</h1>
        <p class="onboarding-intro__lede">Agent X keeps execution local. Start by naming this device and choosing the CLIs it may run.</p>
        <div class="local-note"><span></span><p><strong>Local by default</strong><small>No account or cloud connection is required.</small></p></div>
      </section>

      <section class="onboarding-card" aria-labelledby="setup-title">
        <header>
          <span>1 of 1</span>
          <h2 id="setup-title">Configure this device</h2>
          <p>Detected on ${escapeHTML(platformName(device.os))} · ${escapeHTML(device.arch)}</p>
        </header>

        <form id="onboardingForm">
          <label class="field-label" for="deviceName">Device name</label>
          <input id="deviceName" name="deviceName" maxlength="60" value="${escapeHTML(device.name)}" autocomplete="off" />

          <div class="field-heading">
            <div><strong>Available CLIs</strong><small>Select what Agent X can launch here.</small></div>
            <button class="text-button" id="refreshProviders" type="button">Scan again</button>
          </div>

          <div class="provider-list">${renderProviderRows(state.providers, true)}</div>
          <p class="form-error" id="onboardingError" role="alert"></p>

          <button class="primary-action" type="submit" ${busy ? "disabled" : ""}>
            ${busy ? "Saving…" : "Continue to Agent X"}<span aria-hidden="true">→</span>
          </button>
        </form>
      </section>
    </main>`;

  document.querySelector("#onboardingForm")?.addEventListener("submit", completeOnboarding);
  document.querySelector("#refreshProviders")?.addEventListener("click", refreshProviders);
}

function renderWorkspaceRows(workspaces) {
  if (!workspaces.length) {
    return `<p class="sidebar-empty">No workspaces yet</p>`;
  }
  return workspaces
    .map((workspace) => {
      const provider = providerByID(workspace.providerId);
      const active = workspace.id === selectedWorkspace()?.id ? "is-active" : "";
      return `
        <button class="workspace-row ${active}" type="button" data-workspace-id="${escapeHTML(workspace.id)}">
          <span>${icon("folder")}</span>
          <div><strong>${escapeHTML(workspace.name)}</strong><small>${escapeHTML(provider?.name || workspace.providerId)}</small></div>
          <i aria-hidden="true"></i>
        </button>`;
    })
    .join("");
}

function renderProviderPills() {
  return selectedProviders()
    .map((provider) => `<span class="topbar-provider">${providerIcon(provider)}<strong>${escapeHTML(provider.name)}</strong><i aria-label="Ready"></i></span>`)
    .join("");
}

function renderEmptyWorkspace() {
  const providers = selectedProviders();
  return `
    <section class="empty-workspace">
      <span class="empty-workspace__icon">${icon("folder")}</span>
      <p class="eyebrow">No workspaces yet</p>
      <h1>Create your first workspace.</h1>
      <p>Name the project, choose Claude or Codex, then connect the local folder where the work will run.</p>
      <button class="primary-action primary-action--compact" id="openWorkspace" type="button" ${providers.length ? "" : "disabled"}>
        Create workspace${icon("arrow")}
      </button>
      ${providers.length ? "" : '<small class="empty-workspace__warning">Configure an installed CLI before opening a project.</small>'}
    </section>`;
}

function renderWorkspace(workspace) {
  const provider = providerByID(workspace.providerId);
  return `
    <section class="workspace-home">
      <div class="workspace-home__heading">
        <div>
          <p class="eyebrow">Workspace ready</p>
          <h1>${escapeHTML(workspace.name)}</h1>
          <p class="workspace-path" title="${escapeHTML(workspace.rootPath)}">${escapeHTML(workspace.rootPath)}</p>
        </div>
        <span class="workspace-provider">${providerIcon(provider)}<span><strong>${escapeHTML(provider?.name || workspace.providerId)}</strong><small>Selected provider</small></span></span>
      </div>

      <div class="workspace-ready-card">
        <span class="workspace-ready-card__check">✓</span>
        <div><strong>Project connected</strong><p>Device identity, provider choice, and workspace metadata are persisted locally.</p></div>
      </div>

      <div class="next-slice">
        <span>Next</span>
        <div><strong>Start the provider session</strong><p>The next implementation slice attaches a real PTY to ${escapeHTML(provider?.name || workspace.providerId)} and streams it into chat.</p></div>
      </div>
    </section>`;
}

function availableDevices() {
  const devices = [state.device, ...(state.nearbyDevices || [])];
  return devices.filter((device, index) => devices.findIndex((item) => item.id === device.id) === index);
}

function renderWorkspaceDialog() {
  const providers = selectedProviders();
  const devices = availableDevices();
  return `
    <dialog class="workspace-dialog" id="workspaceDialog" aria-labelledby="workspaceDialogTitle">
      <form id="workspaceForm">
        <header class="workspace-dialog__header">
          <div>
            <p class="eyebrow">New workspace</p>
            <h2 id="workspaceDialogTitle">Set up your project</h2>
            <p>Name the project, choose who runs it, then select its folder.</p>
          </div>
          <button class="dialog-close" id="closeWorkspaceDialog" type="button" aria-label="Close new workspace dialog">${icon("close")}</button>
        </header>

        <div class="workspace-dialog__body">
          <label class="field-label" for="projectName">Project name</label>
          <input class="workspace-name-input" id="projectName" name="projectName" maxlength="80" placeholder="e.g. Booking API" autocomplete="off" required />

          <fieldset class="workspace-fieldset">
            <legend>Run with</legend>
            <p>Choose the CLI that will own the first session.</p>
            <div class="workspace-choice-grid workspace-choice-grid--providers">
              ${providers.map((provider, index) => `
                <label class="workspace-choice workspace-choice--provider">
                  <input type="radio" name="providerId" value="${escapeHTML(provider.id)}" ${index === 0 ? "checked" : ""} required />
                  <span class="workspace-choice__icon">${providerIcon(provider)}</span>
                  <span><strong>${escapeHTML(provider.name)}</strong><small>${escapeHTML(provider.version || provider.description)}</small></span>
                  <i>${icon("check")}</i>
                </label>`).join("")}
            </div>
          </fieldset>

          <fieldset class="workspace-fieldset">
            <legend>${devices.length > 1 ? "Run on device" : "Device"}</legend>
            <p>${devices.length > 1 ? "Choose which trusted device owns the project." : "This project will run locally on this device."}</p>
            <div class="workspace-choice-grid ${devices.length === 1 ? "workspace-choice-grid--single" : ""}">
              ${devices.map((device, index) => `
                <label class="workspace-choice workspace-choice--device">
                  <input type="radio" name="deviceId" value="${escapeHTML(device.id)}" ${index === 0 ? "checked" : ""} required />
                  <span class="workspace-choice__icon workspace-choice__icon--device">${icon("laptop")}</span>
                  <span><strong>${escapeHTML(device.name)}</strong><small>${escapeHTML(platformName(device.os))} · ${device.id === state.device.id ? "This device" : "Nearby"}</small></span>
                  <i>${icon("check")}</i>
                </label>`).join("")}
            </div>
          </fieldset>

          <p class="form-error" id="workspaceFormError" role="alert"></p>
        </div>

        <footer class="workspace-dialog__footer">
          <button class="secondary-action" id="cancelWorkspaceDialog" type="button">Cancel</button>
          <button class="primary-action primary-action--dialog" type="submit" ${providers.length ? "" : "disabled"}>
            Choose project folder${icon("arrow")}
          </button>
        </footer>
      </form>
    </dialog>`;
}

function renderShell() {
  const workspace = selectedWorkspace();
  const workspaces = state.workspaces || [];
  root.innerHTML = `
    <div class="desktop-shell platform-${escapeHTML(state.device.os)}">
      <aside class="app-sidebar">
        <div class="app-brand app-brand--sidebar"><span class="brand-mark">${icon("brand")}</span><strong>Agent X</strong></div>

        <button class="sidebar-search" type="button">${icon("search")}<span>Search projects</span><kbd>⌘K</kbd></button>

        <div class="sidebar-section">
          <p class="sidebar-label">This device</p>
          <div class="device-heading">
            <span class="device-icon">${icon("laptop")}</span>
            <div><strong>${escapeHTML(state.device.name)}</strong><small>${escapeHTML(platformName(state.device.os))} · online</small></div>
            <i aria-label="Online"></i>
          </div>
          <div class="workspace-list">${renderWorkspaceRows(workspaces)}</div>
          <button class="new-workspace" id="newWorkspace" type="button">${icon("plus")}<span>New workspace</span></button>
        </div>

        <div class="sidebar-section sidebar-section--nearby">
          <p class="sidebar-label">Nearby devices</p>
          <div class="nearby-empty"><span></span><p><strong>Scanning locally</strong><small>No trusted devices yet</small></p></div>
        </div>

        <footer class="sidebar-footer">
          <span class="avatar">${icon("brand")}</span>
          <div><strong>Agent X</strong><small>Version ${escapeHTML(state.version)}</small></div>
          <button type="button" aria-label="Settings">${icon("more")}</button>
        </footer>
      </aside>

      <main class="app-main">
        <header class="app-topbar">
          <div class="app-topbar__title"><small>${escapeHTML(state.device.name)}</small><strong>${escapeHTML(workspace?.name || "Workspaces")}</strong></div>
          <div class="app-topbar__providers">${renderProviderPills()}</div>
          <button class="icon-button" id="refreshApp" type="button" aria-label="Refresh provider detection">${icon("refresh")}</button>
        </header>
        ${workspace ? renderWorkspace(workspace) : renderEmptyWorkspace()}
      </main>
    </div>

    ${renderWorkspaceDialog()}

    <div class="app-notice" id="appNotice" role="status" hidden></div>`;

  document.querySelectorAll("[data-workspace-id]").forEach((button) => {
    button.addEventListener("click", () => {
      selectedWorkspaceID = button.dataset.workspaceId;
      renderShell();
    });
  });
  document.querySelector("#newWorkspace")?.addEventListener("click", showWorkspaceDialog);
  document.querySelector("#openWorkspace")?.addEventListener("click", showWorkspaceDialog);
  document.querySelector("#refreshApp")?.addEventListener("click", refreshProviders);
  document.querySelector("#workspaceForm")?.addEventListener("submit", submitWorkspaceForm);
  document.querySelector("#closeWorkspaceDialog")?.addEventListener("click", closeWorkspaceDialog);
  document.querySelector("#cancelWorkspaceDialog")?.addEventListener("click", closeWorkspaceDialog);
}

function render() {
  if (!state) return;
  if (state.needsOnboarding) renderOnboarding();
  else renderShell();
}

function setError(message) {
  const target = document.querySelector("#onboardingError");
  if (target) target.textContent = message || "Something went wrong.";
  else showNotice(message || "Something went wrong.", true);
}

function showNotice(message, isError = false) {
  const notice = document.querySelector("#appNotice");
  if (!notice) return;
  notice.textContent = message;
  notice.classList.toggle("is-error", isError);
  notice.hidden = false;
  window.clearTimeout(noticeTimer);
  noticeTimer = window.setTimeout(() => {
    notice.hidden = true;
  }, 4200);
}

async function completeOnboarding(event) {
  event.preventDefault();
  if (busy) return;
  const form = new FormData(event.currentTarget);
  const selectedProviderIds = form.getAll("provider");
  if (!selectedProviderIds.length) {
    setError("Select at least one installed CLI.");
    return;
  }

  busy = true;
  renderOnboarding();
  try {
    state = await CompleteOnboarding({
      deviceName: String(form.get("deviceName") || "").trim(),
      selectedProviderIds,
    });
    render();
  } catch (error) {
    busy = false;
    renderOnboarding();
    setError(String(error));
  }
}

async function refreshProviders() {
  if (busy) return;
  busy = true;
  try {
    state = await RefreshProviders();
    busy = false;
    render();
    if (!state.needsOnboarding) showNotice("Provider detection refreshed.");
  } catch (error) {
    busy = false;
    render();
    setError(String(error));
  }
}

function showWorkspaceDialog() {
  const providers = selectedProviders();
  if (!providers.length) {
    showNotice("No installed provider is configured.", true);
    return;
  }
  const dialog = document.querySelector("#workspaceDialog");
  dialog?.showModal();
  window.requestAnimationFrame(() => document.querySelector("#projectName")?.focus());
}

function closeWorkspaceDialog() {
  document.querySelector("#workspaceDialog")?.close();
}

async function submitWorkspaceForm(event) {
  event.preventDefault();
  if (busy) return;

  const formElement = event.currentTarget;
  if (!formElement.reportValidity()) return;
  const form = new FormData(formElement);
  const request = {
    name: String(form.get("projectName") || "").trim(),
    providerId: String(form.get("providerId") || ""),
    deviceId: String(form.get("deviceId") || ""),
  };
  if (!request.name) {
    document.querySelector("#workspaceFormError").textContent = "Enter a project name.";
    return;
  }

  closeWorkspaceDialog();
  await new Promise((resolve) => window.requestAnimationFrame(resolve));
  await openWorkspace(request);
}

async function openWorkspace(request) {
  if (busy) return;
  busy = true;
  try {
    const previous = new Map((state.workspaces || []).map((workspace) => [workspace.id, workspace.updatedAt]));
    state = await OpenWorkspace(request);
    const opened = (state.workspaces || []).find(
      (workspace) => !previous.has(workspace.id) || previous.get(workspace.id) !== workspace.updatedAt,
    );
    if (opened) selectedWorkspaceID = opened.id;
    renderShell();
    if (opened) showNotice(`${opened.name} is ready.`);
  } catch (error) {
    renderShell();
    showNotice(String(error), true);
  } finally {
    busy = false;
  }
}

async function start() {
  try {
    state = await Bootstrap();
    selectedWorkspaceID = state.workspaces?.[0]?.id || null;
    render();
  } catch (error) {
    root.innerHTML = `<main class="fatal-error"><span class="brand-mark">${icon("brand")}</span><h1>Agent X could not start.</h1><p>${escapeHTML(String(error))}</p><button type="button" id="retryStart">Try again</button></main>`;
    document.querySelector("#retryStart")?.addEventListener("click", start);
  }
}

start();
