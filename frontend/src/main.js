import "./style.css";
import {
  ApprovePairing,
  Bootstrap,
  CompleteOnboarding,
  DeleteWorkspaceConversation,
  GetWorkspaceSession,
  OpenWorkspace,
  PairingState,
  RefreshProviders,
  RejectPairing,
  RemovePairedDevice,
  ResolveApproval,
  RequestPairing,
  SendMessage,
} from "../wailsjs/go/main/App";
import { BrowserOpenURL, EventsOn } from "../wailsjs/runtime/runtime";
import { groupChatItems, normalizeActivityStatus, summarizeActivityGroup } from "./activity-groups.js";
import { approvalCardModel } from "./approval-view.js";
import { createDeviceSearch } from "./device-search.js";
import { canSubmitComposer, clipboardScreenshot, prepareScreenshot } from "./image-input.js";
import { renderMarkdown } from "./markdown.js";
import { createNoticeState } from "./notice-state.js";
import { commandMatches, expandPromptCommand, parseSlashCommand } from "./slash-commands.js";
import { shouldShowInitialAgentLoader } from "./turn-progress.js";
import {
  configuredProvidersForDevice,
  deviceContext,
  findOpenedWorkspace,
  providersForDevice,
  remoteDeviceByID,
  workspaceEntries,
  workspaceKey,
  workspacesForDevice,
} from "./workspace-routing.js";
import claudeLogo from "./assets/icons/brand/claude.svg";
import codexLogo from "./assets/icons/brand/codex.svg";

const root = document.querySelector("#app");

let state = null;
let selectedDeviceID = null;
let selectedWorkspaceID = null;
let busy = false;
let sessionEventsBound = false;
let deviceEventsBound = false;
let pairingEventsBound = false;
let bridgeEventsBound = false;
let stateEventsBound = false;
let pairingState = { requests: [], pairedDevices: [] };
let pairingBusy = false;
let slashCommandIndex = 0;
const hiddenPairingRequests = new Set();
const deviceSearch = createDeviceSearch({
  durationMs: 30_000,
  onChange() {
    if (state && !state.needsOnboarding) refreshDeviceSections();
  },
});

const sessions = new Map();
const sessionLoads = new Set();
const sendingWorkspaces = new Set();
const deletingConversations = new Set();
const resolvingApprovals = new Set();
const workspaceDrafts = new Map();
const workspaceScreenshotDrafts = new Map();
const workspacePermissionModes = new Map();
const notices = createNoticeState({ onChange: syncNotice });

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
  trash: '<path d="M4 7h16M9 7V4h6v3M7 7l1 13h8l1-13M10 11v5M14 11v5"></path>',
  shield: '<path d="M12 3 20 6v5c0 5-3.4 8.3-8 10-4.6-1.7-8-5-8-10V6l8-3Z"></path><path d="M9.5 12 11 13.5l3.5-4"></path>',
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

function selectedProviders(deviceID = state?.device?.id) {
  return configuredProvidersForDevice(state, deviceID);
}

function providerByID(id, deviceID = selectedDeviceID) {
  return providersForDevice(state, deviceID).find((provider) => provider.id === id);
}

function selectedWorkspace() {
  const workspaces = workspacesForDevice(state, selectedDeviceID);
  return workspaces.find((workspace) => workspace.id === selectedWorkspaceID) || workspaces[0] || null;
}

function sessionForWorkspace(workspaceID, deviceID = selectedDeviceID) {
  return sessions.get(workspaceKey(deviceID, workspaceID)) || null;
}

function cacheRemoteDevices(devices) {
  if (!state) return;
  state.remoteDevices = Array.isArray(devices) ? devices : [];
  for (const remote of state.remoteDevices) {
    for (const snapshot of remote.sessions || []) {
      sessions.set(workspaceKey(remote.device.id, snapshot.workspaceId), snapshot);
    }
  }
}

function selectedWorkspaceKey(workspace = selectedWorkspace()) {
  return workspace ? workspaceKey(selectedDeviceID, workspace.id) : "";
}

function ensureValidWorkspaceSelection() {
  if (!selectedDeviceID) selectedDeviceID = state?.device?.id || null;
  const context = deviceContext(state, selectedDeviceID);
  if (!context) {
    selectedDeviceID = state?.device?.id || null;
    selectedWorkspaceID = state?.workspaces?.[0]?.id || null;
    return;
  }
  const workspaces = context.workspaces || [];
  if (!workspaces.some((workspace) => workspace.id === selectedWorkspaceID)) {
    selectedWorkspaceID = workspaces[0]?.id || null;
  }
}

function textWithBreaks(value = "") {
  return escapeHTML(value).replaceAll("\n", "<br />");
}

function permissionModeForWorkspace(workspace, deviceID = selectedDeviceID) {
  if (workspace?.providerId !== "claude") return "";
  return workspacePermissionModes.get(workspaceKey(deviceID, workspace.id)) || "default";
}

function renderPermissionMode(workspace) {
  if (workspace.providerId !== "claude") {
    return '<span class="composer-access-note">Workspace access</span>';
  }
  const selected = permissionModeForWorkspace(workspace);
  return `
    <label class="composer-access">
      <span class="sr-only">Claude access mode</span>
      <select id="permissionMode" aria-label="Claude access mode" title="Controls what Claude may do in this workspace">
        <option value="default" ${selected === "default" ? "selected" : ""}>Ask before sensitive actions</option>
        <option value="auto" ${selected === "auto" ? "selected" : ""}>Automatic safety review</option>
        <option value="acceptEdits" ${selected === "acceptEdits" ? "selected" : ""}>Edit files</option>
        <option value="plan" ${selected === "plan" ? "selected" : ""}>Plan only</option>
      </select>
    </label>`;
}

function statusLabel(value) {
  if (value === "running") return "Running";
  if (value === "queued") return "Queued";
  if (value === "waiting") return "Needs approval";
  if (value === "failed" || value === "error") return "Failed";
  if (value === "completed") return "Completed";
  return "Ready";
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

function renderWorkspaceRows(workspaces, deviceID) {
  if (!workspaces.length) {
    return `<p class="sidebar-empty">No workspaces yet</p>`;
  }
  return workspaces
    .map((workspace) => {
      const provider = providerByID(workspace.providerId, deviceID);
      const workspaceSession = sessionForWorkspace(workspace.id, deviceID);
      const active = deviceID === selectedDeviceID && workspace.id === selectedWorkspace()?.id ? "is-active" : "";
      const waiting = workspaceSession?.status === "waiting";
      const indicator = waiting ? "is-waiting" : ["running", "queued"].includes(workspaceSession?.status) ? "is-running" : "";
      const indicatorLabel = waiting ? "Approval required" : indicator ? "Agent running" : "Ready";
      return `
        <button class="workspace-row ${active}" type="button" data-device-id="${escapeHTML(deviceID)}" data-workspace-id="${escapeHTML(workspace.id)}">
          <span>${icon("folder")}</span>
          <div><strong>${escapeHTML(workspace.name)}</strong><small>${escapeHTML(provider?.name || workspace.providerId)}</small></div>
          <i class="${indicator}" aria-label="${indicatorLabel}"></i>
        </button>`;
    })
    .join("");
}

function renderProviderPills(deviceID = selectedDeviceID) {
  return selectedProviders(deviceID)
    .map((provider) => `<span class="topbar-provider">${providerIcon(provider)}<strong>${escapeHTML(provider.name)}</strong><i aria-label="Ready"></i></span>`)
    .join("");
}

function updatePairingState(snapshot) {
  pairingState = {
    requests: Array.isArray(snapshot?.requests) ? snapshot.requests : [],
    pairedDevices: Array.isArray(snapshot?.pairedDevices) ? snapshot.pairedDevices : [],
  };
  if (state) state.pairedDevices = pairingState.pairedDevices;
}

function pairedDeviceIDs() {
  const ids = new Set((pairingState.pairedDevices || []).map((device) => device.id));
  for (const device of state?.nearbyDevices || []) {
    if (device.trusted) ids.add(device.id);
  }
  return ids;
}

function renderPairedDevices() {
  const nearby = new Set((state?.nearbyDevices || []).map((device) => device.id));
  const devices = pairingState.pairedDevices || [];
  if (!devices.length) return "";
  return `
    <div class="paired-device-block">
      <p class="sidebar-label">Paired devices</p>
      <div class="nearby-device-list">
        ${devices.map((device) => {
          const remote = remoteDeviceByID(state, device.id);
          const online = Boolean(remote?.online) || nearby.has(device.id);
          return `
            <div class="paired-device-group ${online ? "is-online" : "is-offline"}">
              <div class="nearby-device-row is-paired">
                <span class="nearby-device-row__icon">${icon("laptop")}</span>
                <p><strong>${escapeHTML(device.name)}</strong><small>${escapeHTML(platformName(device.os))} · ${online ? "online" : "offline"}</small></p>
                <button class="device-forget" type="button" data-remove-paired="${escapeHTML(device.id)}" aria-label="Forget ${escapeHTML(device.name)}" ${pairingBusy ? "disabled" : ""}>Forget</button>
              </div>
              <div class="workspace-list workspace-list--remote">
                ${remote?.workspaces?.length ? renderWorkspaceRows(remote.workspaces, device.id) : `<p class="sidebar-empty">${online ? "No workspaces yet" : "Open Agent X to sync projects"}</p>`}
              </div>
            </div>`;
        }).join("")}
      </div>
    </div>`;
}

function renderDeviceSearchStatus(hasDevices) {
  if (deviceSearch.status() === "searching") {
    const seconds = deviceSearch.remainingSeconds();
    return `
      <div class="device-search-status ${hasDevices ? "is-compact" : ""}">
        <span class="device-search-radar" aria-hidden="true"><i></i><i></i><i></i></span>
        <p><strong>Searching nearby</strong><small>Looking for Agent X devices</small></p>
        <time class="device-search-countdown" aria-label="${seconds} seconds remaining">0:${String(seconds).padStart(2, "0")}</time>
      </div>`;
  }
  return `
    <div class="device-search-status is-finished" role="status">
      <span class="device-search-finished">${icon("search")}</span>
      <p><strong>${hasDevices ? "Search finished" : "No other devices found"}</strong><small>Make sure Agent X is open nearby</small></p>
      <button type="button" data-retry-device-search>Retry</button>
    </div>`;
}

function renderNearbyDevices() {
  const paired = pairedDeviceIDs();
  const pending = new Set(
    (pairingState.requests || [])
      .filter((request) => request.direction === "outgoing" && request.status === "pending")
      .map((request) => request.device.id),
  );
  const devices = (state?.nearbyDevices || []).filter((device) => !paired.has(device.id));
  if (!devices.length) {
    return renderDeviceSearchStatus(false);
  }
  return `
    <div class="nearby-device-list">
      ${devices.map((device) => `
        <div class="nearby-device-row">
          <span class="nearby-device-row__icon">${icon("laptop")}</span>
          <p><strong>${escapeHTML(device.name)}</strong><small>${escapeHTML(platformName(device.os))} · Nearby</small></p>
          <button class="device-pair" type="button" ${pending.has(device.id) ? "" : `data-pair-device="${escapeHTML(device.id)}"`} ${pairingBusy || pending.has(device.id) ? "disabled" : ""}>${pending.has(device.id) ? "Waiting" : "Pair"}</button>
        </div>`).join("")}
    </div>
    <p class="nearby-disclaimer">Verify the same code on both devices</p>
    ${renderDeviceSearchStatus(true)}`;
}

function renderDeviceSections() {
  return `
    ${renderPairedDevices()}
    <p class="sidebar-label">Nearby devices</p>
    ${renderNearbyDevices()}`;
}

function bindDeviceSectionActions() {
  document.querySelectorAll("[data-pair-device]").forEach((button) => {
    button.addEventListener("click", () => requestPairing(button.dataset.pairDevice));
  });
  document.querySelectorAll("[data-remove-paired]").forEach((button) => {
    button.addEventListener("click", () => removePairedDevice(button.dataset.removePaired));
  });
  document.querySelectorAll("[data-retry-device-search]").forEach((button) => {
    button.addEventListener("click", () => deviceSearch.retry());
  });
}

function refreshDeviceSections() {
  const target = document.querySelector("#deviceSections");
  if (!target) {
    renderShell();
    return;
  }
  target.innerHTML = renderDeviceSections();
  bindDeviceSectionActions();
}

function currentPairingRequest() {
  const pending = (pairingState.requests || []).filter((request) => request.status === "pending");
  return pending.find((request) => request.direction === "incoming") ||
    pending.find((request) => !hiddenPairingRequests.has(request.id)) || null;
}

function renderPairingDialog() {
  const request = currentPairingRequest();
  if (!request) return "";
  const incoming = request.direction === "incoming";
  return `
    <dialog class="workspace-dialog pairing-dialog" id="pairingDialog" aria-labelledby="pairingDialogTitle">
      <section>
        <header class="workspace-dialog__header pairing-dialog__header">
          <div>
            <p class="eyebrow">${incoming ? "Pairing request" : "Waiting for approval"}</p>
            <h2 id="pairingDialogTitle">${incoming ? `${escapeHTML(request.device.name)} wants to pair` : `Check ${escapeHTML(request.device.name)}`}</h2>
            <p>Confirm that this exact code is visible on both laptops.</p>
          </div>
        </header>
        <div class="pairing-dialog__body">
          <span class="pairing-device-icon">${icon("laptop")}</span>
          <p class="pairing-code-label">Verification code</p>
          <strong class="pairing-code" aria-label="Verification code ${escapeHTML(request.code)}">${escapeHTML(request.code)}</strong>
          <p class="pairing-help">${incoming ? "If the codes match, approve this device. If they do not match, reject it." : `Approve the request on ${escapeHTML(request.device.name)} after checking the code.`}</p>
          ${incoming ? `
            <div class="pairing-actions">
              <button class="secondary-action" type="button" data-reject-pairing="${escapeHTML(request.id)}" ${pairingBusy ? "disabled" : ""}>Reject</button>
              <button class="primary-action primary-action--dialog" type="button" data-approve-pairing="${escapeHTML(request.id)}" ${pairingBusy ? "disabled" : ""}>${pairingBusy ? "Approving…" : "Codes match · Approve"}${icon("check")}</button>
            </div>` : `
            <span class="pairing-wait"><i></i>Waiting for the other laptop</span>
            <button class="pairing-background" type="button" data-hide-pairing="${escapeHTML(request.id)}">Continue in background</button>`}
        </div>
      </section>
    </dialog>`;
}

function renderEmptyWorkspace() {
  const canCreate = workspaceDeviceChoices().some((choice) => choice.online && choice.providers.length);
  return `
    <section class="empty-workspace">
      <span class="empty-workspace__icon">${icon("folder")}</span>
      <p class="eyebrow">No workspaces yet</p>
      <h1>Create your first workspace.</h1>
      <p>Name the project, choose Claude or Codex, then connect the local folder where the work will run.</p>
      <button class="primary-action primary-action--compact" id="openWorkspace" type="button" ${canCreate ? "" : "disabled"}>
        Create workspace${icon("arrow")}
      </button>
      ${canCreate ? "" : '<small class="empty-workspace__warning">Configure an installed CLI before opening a project.</small>'}
    </section>`;
}

function renderChatItems(workspace, workspaceSession, deviceID = selectedDeviceID) {
  const provider = providerByID(workspace.providerId, deviceID);
  const key = workspaceKey(deviceID, workspace.id);
  const showLoader = shouldShowInitialAgentLoader({
    sending: sendingWorkspaces.has(key),
    status: workspaceSession?.status,
    items: workspaceSession?.items || [],
  });

  if (!workspaceSession?.items?.length && !showLoader) {
    return `
      <div class="chat-empty">
        <span>${providerIcon(provider)}</span>
        <h2>Start working with ${escapeHTML(provider?.name || workspace.providerId)}</h2>
        <p>Send a message to begin a resumable session in this workspace.</p>
      </div>`;
  }

  const items = groupChatItems(workspaceSession?.items || [])
    .map((group) => {
      if (group.kind === "activity-group") return renderActivityGroup(group.items);
      const item = group.item;

      if (item.kind === "error") {
        return `<div class="chat-error" role="alert"><strong>Session error</strong><p>${textWithBreaks(item.content)}</p></div>`;
      }

      if (item.kind === "approval") {
        return renderApprovalCard(item, workspace, deviceID);
      }

      if (item.role === "user") {
        return `
          <article class="chat-message chat-message--user">
            <div class="chat-bubble ${item.screenshots?.length ? "has-screenshot" : ""}">
              ${renderMessageScreenshots(item.screenshots)}
              ${item.content ? `<div class="chat-bubble__text">${textWithBreaks(item.content)}</div>` : ""}
            </div>
            ${item.status === "queued" ? '<small class="chat-message__state">Queued</small>' : ""}
          </article>`;
      }

      const provider = providerByID(workspace.providerId, deviceID);
      return `
        <article class="chat-message chat-message--assistant">
          <span class="chat-message__avatar">${providerIcon(provider)}</span>
          <div><strong>${escapeHTML(provider?.name || workspace.providerId)}</strong><div class="chat-copy markdown-body">${renderMarkdown(item.content)}</div></div>
        </article>`;
    })
    .join("");
  return items + (showLoader ? renderAgentLoader(provider, workspaceSession?.status) : "");
}

function renderApprovalCard(item, workspace, deviceID) {
  const view = approvalCardModel(item);
  const approval = item.approval || {};
  const approvalKey = `${workspaceKey(deviceID, workspace.id)}:${item.id}`;
  const resolving = resolvingApprovals.has(approvalKey);
  const detailIsCommand = Boolean(approval.command);
  const paths = (approval.paths || []).filter(Boolean);
  return `
    <section class="approval-card is-${escapeHTML(view.status)}" aria-labelledby="approval-title-${escapeHTML(item.id)}">
      <div class="approval-card__icon">${icon("shield")}</div>
      <div class="approval-card__body">
        <div class="approval-card__meta"><span>${escapeHTML(view.kind)}</span><strong>${escapeHTML(view.result)}</strong></div>
        <h3 id="approval-title-${escapeHTML(item.id)}">${escapeHTML(item.title || "Approval required")}</h3>
        ${detailIsCommand ? `<pre><code>${escapeHTML(approval.command)}</code></pre>` : paths.length ? `<ul>${paths.map((path) => `<li>${escapeHTML(path)}</li>`).join("")}</ul>` : `<p class="approval-card__detail">${escapeHTML(view.detail)}</p>`}
        ${approval.workingDirectory ? `<p class="approval-card__context"><span>Runs in</span>${escapeHTML(approval.workingDirectory)}</p>` : ""}
        ${approval.reason ? `<p class="approval-card__reason">${escapeHTML(approval.reason)}</p>` : ""}
        ${view.pending ? `
          <div class="approval-card__actions">
            <button type="button" class="approval-deny" data-approval-decision="deny" data-approval-id="${escapeHTML(item.id)}" ${resolving ? "disabled" : ""}>Deny</button>
            <button type="button" class="approval-allow" data-approval-decision="allow" data-approval-id="${escapeHTML(item.id)}" ${resolving ? "disabled" : ""}>${resolving ? "Sending…" : "Allow once"}</button>
          </div>` : ""}
      </div>
    </section>`;
}

function screenshotDataURL(screenshot) {
  const mediaType = String(screenshot?.mediaType || "").toLowerCase();
  const data = String(screenshot?.previewData || "").trim();
  if (!["image/png", "image/jpeg", "image/webp"].includes(mediaType) || !/^[a-zA-Z0-9+/]+={0,2}$/.test(data)) {
    return "";
  }
  return `data:${mediaType};base64,${data}`;
}

function renderMessageScreenshots(screenshots) {
  return (screenshots || []).map((screenshot) => {
    const source = screenshotDataURL(screenshot);
    if (!source) return '<span class="chat-screenshot-fallback">Screenshot attached</span>';
    return `<img class="chat-screenshot" src="${escapeHTML(source)}" alt="Pasted screenshot" />`;
  }).join("");
}

function renderScreenshotDraft(screenshot) {
  if (!screenshot) return "";
  const source = screenshotDataURL(screenshot);
  return `
    <div class="composer-screenshot">
      ${source ? `<img src="${escapeHTML(source)}" alt="Screenshot ready to send" />` : ""}
      <span><strong>Screenshot</strong><small>Ready to send</small></span>
      <button type="button" id="removeScreenshot" aria-label="Remove screenshot" title="Remove screenshot">${icon("close")}</button>
    </div>`;
}

function slashCommandMenuOpen(content) {
  return /^\/[a-z-]*$/i.test(String(content || ""));
}

function renderSlashCommandRows(matches) {
  return matches.map((command, index) => `
    <button class="slash-command ${index === slashCommandIndex ? "is-selected" : ""}" type="button" role="option"
      id="slashCommand-${escapeHTML(command.name)}" aria-selected="${index === slashCommandIndex}" data-slash-command="${escapeHTML(command.name)}">
      <span class="slash-command__icon">${icon("terminal")}</span>
      <span><strong>/${escapeHTML(command.name)} <i>${escapeHTML(command.title)}</i></strong><small>${escapeHTML(command.description)}</small></span>
      <kbd>↵</kbd>
    </button>`).join("");
}

function renderSlashCommandMenu(content, providerID) {
  const open = slashCommandMenuOpen(content);
  const matches = open ? commandMatches(content, providerID) : [];
  return `
    <section class="slash-command-menu" id="slashCommandMenu" role="listbox" aria-label="Commands" ${open ? "" : "hidden"}>
      <header><strong>Commands</strong><small>${escapeHTML(providerByID(providerID)?.name || providerID)}</small></header>
      <div class="slash-command-list" id="slashCommandList">
        ${matches.length ? renderSlashCommandRows(matches) : '<p class="slash-command-empty">No Agent X command matches. Press Enter to send it to the provider.</p>'}
      </div>
    </section>`;
}

function updateSlashCommandMenu(input, workspace) {
  const menu = document.querySelector("#slashCommandMenu");
  const list = document.querySelector("#slashCommandList");
  if (!menu || !list || !workspace) return;
  const open = slashCommandMenuOpen(input.value);
  const matches = open ? commandMatches(input.value, workspace.providerId) : [];
  slashCommandIndex = 0;
  menu.hidden = !open;
  list.innerHTML = matches.length
    ? renderSlashCommandRows(matches)
    : '<p class="slash-command-empty">No Agent X command matches. Press Enter to send it to the provider.</p>';
  input.setAttribute("aria-expanded", String(open));
  input.setAttribute("aria-activedescendant", matches[0] ? `slashCommand-${matches[0].name}` : "");
}

function moveSlashCommandSelection(input, direction) {
  const rows = Array.from(document.querySelectorAll("[data-slash-command]"));
  if (!rows.length) return;
  slashCommandIndex = (slashCommandIndex + direction + rows.length) % rows.length;
  rows.forEach((row, index) => {
    const selected = index === slashCommandIndex;
    row.classList.toggle("is-selected", selected);
    row.setAttribute("aria-selected", String(selected));
  });
  const selected = rows[slashCommandIndex];
  input.setAttribute("aria-activedescendant", selected.id);
  selected.scrollIntoView({ block: "nearest" });
}

function selectSlashCommand(name) {
  const workspace = selectedWorkspace();
  const input = document.querySelector("#chatInput");
  if (!workspace || !input || !name) return;
  const value = `/${name} `;
  workspaceDrafts.set(selectedWorkspaceKey(workspace), value);
  input.value = value;
  resizeChatInput(input);
  updateSlashCommandMenu(input, workspace);
  document.querySelector("#chatForm button[type='submit']")?.removeAttribute("disabled");
  input.focus();
  input.setSelectionRange(value.length, value.length);
}

function renderAgentLoader(provider, status) {
  const providerName = provider?.name || "Agent";
  const label = status === "queued" ? "Waiting in queue…" : `${providerName} is thinking…`;
  return `
    <div class="agent-loader" role="status" aria-live="polite">
      <span class="chat-message__avatar">${providerIcon(provider)}</span>
      <div>
        <strong>${escapeHTML(providerName)}</strong>
        <span class="agent-loader__line"><i></i><i></i><i></i><small>${escapeHTML(label)}</small></span>
      </div>
    </div>`;
}

function renderActivityGroup(items) {
  const summary = summarizeActivityGroup(items);
  return `
    <details class="tool-group is-${escapeHTML(summary.status)}">
      <summary class="tool-group__summary">
        <span class="tool-group__status" aria-hidden="true"></span>
        <span><strong>${escapeHTML(summary.title)}</strong><small>${escapeHTML(summary.detail)}</small></span>
        ${icon("chevron")}
      </summary>
      <div class="tool-group__steps">
        ${items.map((item) => {
          const hasContent = Boolean(item.content);
          const itemStatus = normalizeActivityStatus(item.status);
          const body = `
            <span class="tool-step__status" aria-hidden="true"></span>
            <strong>${escapeHTML(item.title || "Agent activity")}</strong>
            <small>${escapeHTML(statusLabel(itemStatus))}</small>
            ${hasContent ? icon("chevron") : ""}`;
          if (!hasContent) {
            return `<div class="tool-step is-${escapeHTML(itemStatus)}">${body}</div>`;
          }
          return `
            <details class="tool-step is-${escapeHTML(itemStatus)}">
              <summary>${body}</summary>
              <pre>${escapeHTML(item.content)}</pre>
            </details>`;
        }).join("")}
      </div>
    </details>`;
}

function renderWorkspace(workspace) {
  const owner = deviceContext(state, selectedDeviceID);
  const key = workspaceKey(selectedDeviceID, workspace.id);
  const provider = providerByID(workspace.providerId, selectedDeviceID);
  const workspaceSession = sessionForWorkspace(workspace.id, selectedDeviceID);
  const isLoading = sessionLoads.has(key) && !workspaceSession;
  const draft = workspaceDrafts.get(key) || "";
  const screenshotDraft = workspaceScreenshotDrafts.get(key) || null;
  const running = ["running", "queued", "waiting"].includes(workspaceSession?.status);
  const queueDepth = workspaceSession?.queueDepth || 0;
  const deleting = deletingConversations.has(key);
  return `
    <section class="chat-workspace" aria-label="${escapeHTML(workspace.name)} chat">
      <div class="chat-scroll" id="chatScroll">
        <div class="chat-column">
          <header class="chat-context">
            <div><h1>${escapeHTML(workspace.name)}</h1><p title="${escapeHTML(workspace.rootPath)}">${escapeHTML(owner?.device?.name || "Device")} · ${escapeHTML(workspace.rootPath)}</p></div>
            <div class="chat-context__actions">
              <span class="session-status is-${escapeHTML(workspaceSession?.status || "idle")}"><i></i>${escapeHTML(statusLabel(workspaceSession?.status))}</span>
              <button class="delete-conversation" id="deleteConversation" type="button" aria-label="Delete conversation and start fresh" title="Delete conversation and start fresh" ${running || isLoading || deleting ? "disabled" : ""}>${icon("trash")}</button>
            </div>
          </header>
          <div class="chat-items" id="chatItems">
            ${isLoading ? '<div class="chat-loading">Loading session…</div>' : renderChatItems(workspace, workspaceSession, selectedDeviceID)}
          </div>
        </div>
      </div>

      <footer class="chat-composer-shell">
        <div class="chat-composer-stack">
          ${renderSlashCommandMenu(draft, workspace.providerId)}
          <form class="chat-composer" id="chatForm">
            ${renderScreenshotDraft(screenshotDraft)}
            <textarea id="chatInput" name="message" rows="1" maxlength="100000" placeholder="Message ${escapeHTML(provider?.name || workspace.providerId)}…" aria-label="Message ${escapeHTML(provider?.name || workspace.providerId)}" aria-autocomplete="list" aria-controls="slashCommandMenu" aria-expanded="${slashCommandMenuOpen(draft)}">${escapeHTML(draft)}</textarea>
            <div class="chat-composer__footer">
              <div class="chat-composer__meta">
                ${renderPermissionMode(workspace)}
                <span class="composer-run-state ${running ? "is-running" : ""}" aria-live="polite">${running ? `${queueDepth ? `${queueDepth} queued · ` : ""}${escapeHTML(provider?.name || workspace.providerId)} is working on ${escapeHTML(owner?.device?.name || "device")}` : `Runs on ${escapeHTML(owner?.device?.name || "this device")}`}</span>
                ${running ? "" : '<span class="composer-paste-hint">Type / for commands · Paste a screenshot</span>'}
              </div>
              <button type="submit" aria-label="Send message" ${sendingWorkspaces.has(key) || !canSubmitComposer(draft, screenshotDraft) ? "disabled" : ""}>${icon("arrow")}</button>
            </div>
          </form>
        </div>
      </footer>
    </section>`;
}

function workspaceDeviceChoices() {
  const localProviders = configuredProvidersForDevice(state, state.device.id);
  const choices = [{ device: state.device, online: true, local: true, providers: localProviders }];
  for (const paired of pairingState.pairedDevices || []) {
    const remote = remoteDeviceByID(state, paired.id);
    choices.push({
      device: paired,
      online: Boolean(remote?.online),
      local: false,
      providers: configuredProvidersForDevice(state, paired.id),
    });
  }
  return choices;
}

function renderWorkspaceProviderChoices(deviceID) {
  const providers = configuredProvidersForDevice(state, deviceID);
  if (!providers.length) {
    return '<p class="workspace-choice-empty">No configured CLI is available on this device.</p>';
  }
  return providers.map((provider, index) => `
    <label class="workspace-choice workspace-choice--provider">
      <input type="radio" name="providerId" value="${escapeHTML(provider.id)}" ${index === 0 ? "checked" : ""} required />
      <span class="workspace-choice__icon">${providerIcon(provider)}</span>
      <span><strong>${escapeHTML(provider.name)}</strong><small>${escapeHTML(provider.version || provider.description)}</small></span>
      <i>${icon("check")}</i>
    </label>`).join("");
}

function renderWorkspaceDialog() {
  const choices = workspaceDeviceChoices();
  const initial = choices.find((choice) => choice.local && choice.providers.length) ||
    choices.find((choice) => choice.online && choice.providers.length) || choices[0];
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
            <legend>Project device</legend>
            <p>The folder picker and agent will run on the device you choose.</p>
            <div class="workspace-choice-grid workspace-choice-grid--devices">
              ${choices.map((choice) => {
                const unavailable = !choice.online || !choice.providers.length;
                const detail = choice.local ? "This device" : choice.online ? "Connected" : "Offline";
                return `
                  <label class="workspace-choice workspace-choice--device ${unavailable ? "is-disabled" : ""}">
                    <input type="radio" name="deviceId" value="${escapeHTML(choice.device.id)}" ${choice.device.id === initial.device.id ? "checked" : ""} ${unavailable ? "disabled" : ""} required />
                    <span class="workspace-choice__icon workspace-choice__icon--device">${icon("laptop")}</span>
                    <span><strong>${escapeHTML(choice.device.name)}</strong><small>${escapeHTML(platformName(choice.device.os))} · ${detail}</small></span>
                    <i>${icon("check")}</i>
                  </label>`;
              }).join("")}
            </div>
          </fieldset>

          <fieldset class="workspace-fieldset">
            <legend>Run with</legend>
            <p>Only CLIs configured on the selected device are shown.</p>
            <div class="workspace-choice-grid workspace-choice-grid--providers" id="workspaceProviderChoices">
              ${renderWorkspaceProviderChoices(initial.device.id)}
            </div>
          </fieldset>

          <p class="form-error" id="workspaceFormError" role="alert"></p>
        </div>

        <footer class="workspace-dialog__footer">
          <button class="secondary-action" id="cancelWorkspaceDialog" type="button">Cancel</button>
          <button class="primary-action primary-action--dialog" id="chooseWorkspaceFolder" type="submit" ${initial.online && initial.providers.length ? "" : "disabled"}>
            Choose folder on ${escapeHTML(initial.device.name)}${icon("arrow")}
          </button>
        </footer>
      </form>
    </dialog>`;
}

function renderShell() {
  ensureValidWorkspaceSelection();
  const workspace = selectedWorkspace();
  const workspaces = state.workspaces || [];
  const owner = deviceContext(state, selectedDeviceID);
  const notice = notices.current();
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
          <div class="workspace-list">${renderWorkspaceRows(workspaces, state.device.id)}</div>
        </div>

        <div class="sidebar-section sidebar-section--nearby" id="deviceSections">
          ${renderDeviceSections()}
        </div>

        <footer class="sidebar-footer">
          <button class="new-workspace new-workspace--footer" id="newWorkspace" type="button">
            <span>${icon("plus")}</span><strong>New workspace</strong>${icon("arrow")}
          </button>
        </footer>
      </aside>

      <main class="app-main">
        <header class="app-topbar">
          <div class="app-topbar__title"><small>${escapeHTML(owner?.device?.name || state.device.name)}</small><strong>${escapeHTML(workspace?.name || "Workspaces")}</strong></div>
          <div class="app-topbar__providers">${renderProviderPills(selectedDeviceID)}</div>
          <button class="icon-button" id="refreshApp" type="button" aria-label="Refresh provider detection">${icon("refresh")}</button>
        </header>
        ${workspace ? renderWorkspace(workspace) : renderEmptyWorkspace()}
      </main>
    </div>

    ${renderWorkspaceDialog()}
    ${renderPairingDialog()}

    <div class="app-notice ${notice?.isError ? "is-error" : ""}" id="appNotice" role="status" ${notice ? "" : "hidden"}>${notice ? escapeHTML(notice.message) : ""}</div>`;

  document.querySelectorAll("[data-workspace-id]").forEach((button) => {
    button.addEventListener("click", () => {
      selectedDeviceID = button.dataset.deviceId;
      selectedWorkspaceID = button.dataset.workspaceId;
      renderShell();
      loadWorkspaceSession(selectedDeviceID, selectedWorkspaceID);
    });
  });
  document.querySelector("#newWorkspace")?.addEventListener("click", showWorkspaceDialog);
  document.querySelector("#openWorkspace")?.addEventListener("click", showWorkspaceDialog);
  document.querySelector("#refreshApp")?.addEventListener("click", refreshProviders);
  document.querySelector("#workspaceForm")?.addEventListener("submit", submitWorkspaceForm);
  document.querySelectorAll("input[name='deviceId']").forEach((input) => {
    input.addEventListener("change", updateWorkspaceProviderChoices);
  });
  document.querySelector("#closeWorkspaceDialog")?.addEventListener("click", closeWorkspaceDialog);
  document.querySelector("#cancelWorkspaceDialog")?.addEventListener("click", closeWorkspaceDialog);
  document.querySelector("#chatForm")?.addEventListener("submit", submitChatMessage);
  document.querySelector("#slashCommandMenu")?.addEventListener("click", (event) => {
    const command = event.target.closest("[data-slash-command]");
    if (command) selectSlashCommand(command.dataset.slashCommand);
  });
  document.querySelector("#removeScreenshot")?.addEventListener("click", removeScreenshotDraft);
  document.querySelector("#deleteConversation")?.addEventListener("click", deleteConversation);
  document.querySelectorAll("[data-approval-decision]").forEach((button) => {
    button.addEventListener("click", () => resolveWorkspaceApproval(button.dataset.approvalId, button.dataset.approvalDecision));
  });
  bindDeviceSectionActions();
  document.querySelector("[data-approve-pairing]")?.addEventListener("click", (event) => {
    approvePairing(event.currentTarget.dataset.approvePairing);
  });
  document.querySelector("[data-reject-pairing]")?.addEventListener("click", (event) => {
    rejectPairing(event.currentTarget.dataset.rejectPairing);
  });
  document.querySelector("[data-hide-pairing]")?.addEventListener("click", (event) => {
    hiddenPairingRequests.add(event.currentTarget.dataset.hidePairing);
    renderShell();
  });
  document.querySelector("#permissionMode")?.addEventListener("change", (event) => {
    const workspace = selectedWorkspace();
    if (workspace?.providerId === "claude") {
      workspacePermissionModes.set(selectedWorkspaceKey(workspace), event.currentTarget.value);
    }
  });
  document.querySelectorAll(".markdown-body a[href]").forEach((link) => {
    link.addEventListener("click", (event) => {
      event.preventDefault();
      BrowserOpenURL(event.currentTarget.href);
    });
  });
  const chatInput = document.querySelector("#chatInput");
  chatInput?.addEventListener("input", () => {
    const workspace = selectedWorkspace();
    if (!workspace) return;
    const key = selectedWorkspaceKey(workspace);
    workspaceDrafts.set(key, chatInput.value);
    resizeChatInput(chatInput);
    updateSlashCommandMenu(chatInput, workspace);
    document.querySelector("#chatForm button[type='submit']")?.toggleAttribute(
      "disabled",
      sendingWorkspaces.has(key) || !canSubmitComposer(chatInput.value, workspaceScreenshotDrafts.get(key)),
    );
  });
  chatInput?.addEventListener("paste", handleScreenshotPaste);
  chatInput?.addEventListener("keydown", (event) => {
    const commandMenu = document.querySelector("#slashCommandMenu");
    const commandRows = Array.from(document.querySelectorAll("[data-slash-command]"));
    if (!commandMenu?.hidden && commandRows.length) {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        moveSlashCommandSelection(chatInput, event.key === "ArrowDown" ? 1 : -1);
        return;
      }
      if ((event.key === "Enter" && !event.shiftKey) || event.key === "Tab") {
        event.preventDefault();
        selectSlashCommand(commandRows[slashCommandIndex]?.dataset.slashCommand);
        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        commandMenu.hidden = true;
        chatInput.setAttribute("aria-expanded", "false");
        return;
      }
    }
    if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      document.querySelector("#chatForm")?.requestSubmit();
    }
  });
  if (chatInput) resizeChatInput(chatInput);
  scrollChatToEnd();
  document.querySelector("#pairingDialog")?.showModal();
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
  notices.show(message, isError);
}

function syncNotice(currentNotice) {
  const target = document.querySelector("#appNotice");
  if (!target) return;
  target.textContent = currentNotice?.message || "";
  target.classList.toggle("is-error", Boolean(currentNotice?.isError));
  target.hidden = !currentNotice;
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
    updatePairingState(await PairingState());
    busy = false;
    deviceSearch.start();
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

async function requestPairing(deviceID) {
  if (!deviceID || pairingBusy) return;
  pairingBusy = true;
  renderShell();
  let message = "Pairing request sent.";
  let isError = false;
  try {
    updatePairingState(await RequestPairing(deviceID));
  } catch (error) {
    message = String(error);
    isError = true;
  } finally {
    pairingBusy = false;
    renderShell();
    showNotice(message, isError);
  }
}

async function approvePairing(requestID) {
  if (!requestID || pairingBusy) return;
  pairingBusy = true;
  renderShell();
  let message = "Device paired. It will reconnect automatically.";
  let isError = false;
  try {
    updatePairingState(await ApprovePairing(requestID));
  } catch (error) {
    message = String(error);
    isError = true;
  } finally {
    pairingBusy = false;
    renderShell();
    showNotice(message, isError);
  }
}

async function rejectPairing(requestID) {
  if (!requestID || pairingBusy) return;
  pairingBusy = true;
  let message = "Pairing request rejected.";
  let isError = false;
  try {
    updatePairingState(await RejectPairing(requestID));
  } catch (error) {
    message = String(error);
    isError = true;
  } finally {
    pairingBusy = false;
    renderShell();
    showNotice(message, isError);
  }
}

async function removePairedDevice(deviceID) {
  if (!deviceID || pairingBusy) return;
  pairingBusy = true;
  let message = "Device forgotten.";
  let isError = false;
  try {
    updatePairingState(await RemovePairedDevice(deviceID));
  } catch (error) {
    message = String(error);
    isError = true;
  } finally {
    pairingBusy = false;
    renderShell();
    showNotice(message, isError);
  }
}

function showWorkspaceDialog() {
  const available = workspaceDeviceChoices().some((choice) => choice.online && choice.providers.length);
  if (!available) {
    showNotice("No connected device has a configured CLI.", true);
    return;
  }
  const dialog = document.querySelector("#workspaceDialog");
  dialog?.showModal();
  window.requestAnimationFrame(() => document.querySelector("#projectName")?.focus());
}

function updateWorkspaceProviderChoices(event) {
  const deviceID = event.currentTarget.value;
  const providers = configuredProvidersForDevice(state, deviceID);
  const device = deviceContext(state, deviceID)?.device;
  const providerChoices = document.querySelector("#workspaceProviderChoices");
  if (providerChoices) providerChoices.innerHTML = renderWorkspaceProviderChoices(deviceID);
  const submit = document.querySelector("#chooseWorkspaceFolder");
  if (submit) {
    submit.disabled = !providers.length;
    submit.innerHTML = `Choose folder on ${escapeHTML(device?.name || "device")}${icon("arrow")}`;
  }
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
    const previous = new Map(workspaceEntries(state).map((entry) => [entry.key, entry.workspace.updatedAt]));
    const target = deviceContext(state, request.deviceId)?.device;
    if (request.deviceId !== state.device.id) {
      renderShell();
      showNotice(`Choose the project folder on ${target?.name || "the paired device"}.`);
    }
    state = await OpenWorkspace(request);
    cacheRemoteDevices(state.remoteDevices);
    const opened = findOpenedWorkspace(previous, state);
    if (opened) {
      selectedDeviceID = opened.deviceID;
      selectedWorkspaceID = opened.workspace.id;
    }
    renderShell();
    if (opened) {
      showNotice(`${opened.workspace.name} is ready on ${deviceContext(state, opened.deviceID)?.device?.name || "the device"}.`);
      await loadWorkspaceSession(opened.deviceID, opened.workspace.id);
    } else {
      showNotice("Folder selection was cancelled.");
    }
  } catch (error) {
    renderShell();
    showNotice(String(error), true);
  } finally {
    busy = false;
  }
}

function resizeChatInput(input) {
  input.style.height = "auto";
  input.style.height = `${Math.min(input.scrollHeight, 180)}px`;
}

function scrollChatToEnd() {
  window.requestAnimationFrame(() => {
    const target = document.querySelector("#chatScroll");
    if (target) target.scrollTop = target.scrollHeight;
  });
}

async function loadWorkspaceSession(deviceID, workspaceID) {
  const key = workspaceKey(deviceID, workspaceID);
  if (!deviceID || !workspaceID || sessionLoads.has(key)) return;
  sessionLoads.add(key);
  if (key === selectedWorkspaceKey()) renderShell();
  try {
    const snapshot = await GetWorkspaceSession({ deviceId: deviceID, workspaceId: workspaceID });
    sessions.set(key, snapshot);
  } catch (error) {
    showNotice(String(error), true);
  } finally {
    sessionLoads.delete(key);
    if (key === selectedWorkspaceKey()) renderShell();
  }
}

async function submitChatMessage(event) {
  event.preventDefault();
  const workspace = selectedWorkspace();
  const deviceID = selectedDeviceID;
  const key = workspaceKey(deviceID, workspace?.id);
  if (!workspace || sendingWorkspaces.has(key)) return;
  const displayContent = (workspaceDrafts.get(key) || "").trim();
  const screenshot = workspaceScreenshotDrafts.get(key) || null;
  if (!canSubmitComposer(displayContent, screenshot)) return;

  const command = parseSlashCommand(displayContent, workspace.providerId);
  if (command?.kind === "action") {
    workspaceDrafts.set(key, "");
    renderShell();
    await executeSlashCommandAction(command.command, workspace, deviceID);
    return;
  }
  const content = command?.kind === "prompt"
    ? expandPromptCommand(command.command, command.arguments, workspace.providerId)
    : displayContent;

  sendingWorkspaces.add(key);
  workspaceDrafts.set(key, "");
  workspaceScreenshotDrafts.delete(key);
  renderShell();
  try {
    const snapshot = await SendMessage({
      deviceId: deviceID,
      workspaceId: workspace.id,
      content,
      displayContent: command?.kind === "prompt" ? displayContent : "",
      permissionMode: permissionModeForWorkspace(workspace, deviceID),
      screenshot: screenshot ? {
        mediaType: screenshot.mediaType,
        data: screenshot.data,
        previewData: screenshot.previewData,
      } : null,
    });
    sessions.set(key, snapshot);
  } catch (error) {
    workspaceDrafts.set(key, displayContent);
    if (screenshot) workspaceScreenshotDrafts.set(key, screenshot);
    const deviceName = deviceContext(state, deviceID)?.device?.name || "the selected device";
    const detail = String(error).replace(/^Error:\s*/i, "");
    showNotice(`Couldn’t send to ${deviceName}. ${detail} Your message is still in the box.`, true);
  } finally {
    sendingWorkspaces.delete(key);
    if (key === selectedWorkspaceKey()) renderShell();
  }
}

async function executeSlashCommandAction(command, workspace, deviceID) {
  const key = workspaceKey(deviceID, workspace.id);
  if (command === "clear") {
    await deleteConversation();
    return;
  }
  if (command === "status") {
    const snapshot = sessionForWorkspace(workspace.id, deviceID);
    const device = deviceContext(state, deviceID)?.device;
    const provider = providerByID(workspace.providerId, deviceID);
    showNotice(`${provider?.name || workspace.providerId} is ${statusLabel(snapshot?.status).toLowerCase()} on ${device?.name || "this device"}.`);
    return;
  }
  const permissionModes = { ask: "default", plan: "plan", auto: "auto", "accept-edits": "acceptEdits" };
  const mode = permissionModes[command];
  if (workspace.providerId === "claude" && mode) {
    workspacePermissionModes.set(key, mode);
    renderShell();
    const label = command === "accept-edits" ? "accept edits" : command === "ask" ? "ask before sensitive actions" : command;
    showNotice(`Claude access changed to ${label}.`);
  }
}

async function resolveWorkspaceApproval(approvalID, decision) {
  const workspace = selectedWorkspace();
  const deviceID = selectedDeviceID;
  if (!workspace || !approvalID || !["allow", "deny"].includes(decision)) return;
  const key = workspaceKey(deviceID, workspace.id);
  const approvalKey = `${key}:${approvalID}`;
  if (resolvingApprovals.has(approvalKey)) return;

  resolvingApprovals.add(approvalKey);
  renderShell();
  try {
    const snapshot = await ResolveApproval({
      deviceId: deviceID,
      workspaceId: workspace.id,
      approvalId: approvalID,
      decision,
    });
    sessions.set(key, snapshot);
  } catch (error) {
    showNotice(String(error), true);
    await loadWorkspaceSession(deviceID, workspace.id);
  } finally {
    resolvingApprovals.delete(approvalKey);
    if (key === selectedWorkspaceKey()) renderShell();
  }
}

async function handleScreenshotPaste(event) {
  const file = clipboardScreenshot(event.clipboardData);
  if (!file) return;
  event.preventDefault();

  const workspace = selectedWorkspace();
  if (!workspace) return;
  const key = selectedWorkspaceKey(workspace);
  try {
    const screenshot = await prepareScreenshot(file);
    workspaceScreenshotDrafts.set(key, screenshot);
    if (key === selectedWorkspaceKey()) {
      renderShell();
      const input = document.querySelector("#chatInput");
      input?.focus();
      input?.setSelectionRange(input.value.length, input.value.length);
    }
  } catch (error) {
    showNotice(String(error), true);
  }
}

function removeScreenshotDraft() {
  const workspace = selectedWorkspace();
  if (!workspace) return;
  workspaceScreenshotDrafts.delete(selectedWorkspaceKey(workspace));
  renderShell();
  document.querySelector("#chatInput")?.focus();
}

async function deleteConversation() {
  const workspace = selectedWorkspace();
  const deviceID = selectedDeviceID;
  const key = workspaceKey(deviceID, workspace?.id);
  if (!workspace || deletingConversations.has(key)) return;
  const workspaceSession = sessionForWorkspace(workspace.id, deviceID);
  if (["running", "queued", "waiting"].includes(workspaceSession?.status)) {
    showNotice("Wait for the current run to finish.", true);
    return;
  }

  deletingConversations.add(key);
  renderShell();
  try {
    const snapshot = await DeleteWorkspaceConversation({ deviceId: deviceID, workspaceId: workspace.id });
    sessions.set(key, snapshot);
    workspaceDrafts.delete(key);
    workspaceScreenshotDrafts.delete(key);
    showNotice("Conversation deleted. Project files were not changed.");
  } catch (error) {
    showNotice(String(error), true);
  } finally {
    deletingConversations.delete(key);
    if (key === selectedWorkspaceKey()) renderShell();
  }
}

function bindSessionEvents() {
  if (sessionEventsBound) return;
  sessionEventsBound = true;
  EventsOn("agentx:session", (snapshot) => {
    if (!snapshot?.workspaceId) return;
    sessions.set(workspaceKey(state?.device?.id, snapshot.workspaceId), snapshot);
    if (state && !state.needsOnboarding && !document.querySelector("#workspaceDialog")?.open) renderShell();
  });
}

function bindBridgeEvents() {
  if (bridgeEventsBound) return;
  bridgeEventsBound = true;
  EventsOn("agentx:bridge", (snapshot) => {
    if (!state) return;
    cacheRemoteDevices(snapshot?.devices);
    ensureValidWorkspaceSelection();
    if (!state.needsOnboarding && !document.querySelector("#workspaceDialog")?.open) renderShell();
  });
}

function bindStateEvents() {
  if (stateEventsBound) return;
  stateEventsBound = true;
  EventsOn("agentx:state", (snapshot) => {
    if (!snapshot) return;
    state = snapshot;
    cacheRemoteDevices(snapshot.remoteDevices);
    pairingState.pairedDevices = Array.isArray(snapshot.pairedDevices)
      ? snapshot.pairedDevices
      : pairingState.pairedDevices;
    ensureValidWorkspaceSelection();
    if (!state.needsOnboarding && !document.querySelector("#workspaceDialog")?.open) renderShell();
  });
}

function bindDeviceEvents() {
  if (deviceEventsBound) return;
  deviceEventsBound = true;
  EventsOn("agentx:devices", (devices) => {
    if (!state) return;
    state.nearbyDevices = Array.isArray(devices) ? devices : [];
    if (!state.needsOnboarding) refreshDeviceSections();
  });
}

function bindPairingEvents() {
  if (pairingEventsBound) return;
  pairingEventsBound = true;
  EventsOn("agentx:pairing", (snapshot) => {
    const previous = new Map((pairingState.requests || []).map((request) => [request.id, request.status]));
    const changed = (snapshot?.requests || []).find(
      (request) => previous.has(request.id) && previous.get(request.id) !== request.status,
    );
    updatePairingState(snapshot);
    if (!state?.needsOnboarding) {
      renderShell();
      if (changed?.status === "approved") showNotice("Device paired. It will reconnect automatically.");
      else if (changed?.status === "rejected") showNotice("Pairing request was rejected.", true);
      else if (["expired", "failed"].includes(changed?.status)) showNotice("Pairing request expired. Try again.", true);
    }
  });
}

async function start() {
  try {
    bindSessionEvents();
    bindBridgeEvents();
    bindStateEvents();
    bindDeviceEvents();
    bindPairingEvents();
    state = await Bootstrap();
    cacheRemoteDevices(state.remoteDevices);
    updatePairingState(await PairingState());
    selectedDeviceID = state.device?.id || null;
    selectedWorkspaceID = state.workspaces?.[0]?.id || null;
    if (!state.needsOnboarding) deviceSearch.start();
    render();
    if (selectedWorkspaceID) await loadWorkspaceSession(selectedDeviceID, selectedWorkspaceID);
  } catch (error) {
    root.innerHTML = `<main class="fatal-error"><span class="brand-mark">${icon("brand")}</span><h1>Agent X could not start.</h1><p>${escapeHTML(String(error))}</p><button type="button" id="retryStart">Try again</button></main>`;
    document.querySelector("#retryStart")?.addEventListener("click", start);
  }
}

start();
