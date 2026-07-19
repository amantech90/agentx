/* Agent X — self-driving UI demo.
   Renders the real app markup (real app.css) and choreographs a scripted
   session: type → send → agent works → reply → device switch → follow-up. */

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
  trash: '<path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6 7l1 13a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-13"></path>',
};

const icon = (name) =>
  `<svg class="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${iconPaths[name] || iconPaths.terminal}</svg>`;

const claudeLogo = `<img class="provider-logo" src="claude.svg" alt="" />`;
const codexLogo = `<img class="provider-logo" src="codex.svg" alt="" />`;

const MAC = "Aman’s MacBook";
const PC = "Work PC";

const PROJECTS = {
  mac: {
    name: "Payments API",
    path: "~/dev/payments-api",
    provider: "Claude Code",
    logo: claudeLogo,
    ownerLabel: "this device",
    device: MAC,
  },
  pc: {
    name: "Inventory Sync",
    path: "C:\\work\\inventory-sync",
    provider: "Codex",
    logo: codexLogo,
    ownerLabel: PC,
    device: PC,
  },
};

const state = {
  view: "mac", // which project/device is selected
  status: "idle", // idle | running | completed
  running: false,
};

const root = document.querySelector("#app");

function statusLabel(value) {
  if (value === "running") return "Running";
  if (value === "completed") return "Completed";
  return "Ready";
}

function renderShell() {
  const project = PROJECTS[state.view];
  const runningNote = `${project.provider} is working on ${project.ownerLabel}`;

  root.innerHTML = `
    <div class="desktop-shell platform-darwin">
      <aside class="app-sidebar">
        <div class="app-brand app-brand--sidebar"><span class="brand-mark">${icon("brand")}</span><strong>Agent X</strong></div>

        <button class="sidebar-search" type="button">${icon("search")}<span>Search projects</span><kbd>⌘K</kbd></button>

        <div class="sidebar-section">
          <p class="sidebar-label">This device</p>
          <div class="device-heading">
            <span class="device-icon">${icon("laptop")}</span>
            <div><strong>${MAC}</strong><small>macOS · online</small></div>
            <i aria-label="Online"></i>
          </div>
          <div class="workspace-list">
            <button class="workspace-row ${state.view === "mac" ? "is-active" : ""}" type="button">
              <span>${icon("folder")}</span>
              <div><strong>${PROJECTS.mac.name}</strong><small>${PROJECTS.mac.provider}</small></div>
              <i id="wsDotMac" class="${state.view === "mac" && state.running ? "is-running" : ""}"></i>
            </button>
          </div>
        </div>

        <div class="sidebar-section sidebar-section--nearby">
          <div class="paired-device-block">
            <p class="sidebar-label">Paired devices</p>
            <div class="nearby-device-list">
              <div class="paired-device-group is-online">
                <div class="nearby-device-row is-paired">
                  <span class="nearby-device-row__icon">${icon("laptop")}</span>
                  <p><strong>${PC}</strong><small>Windows · online</small></p>
                </div>
                <div class="workspace-list workspace-list--remote">
                  <button class="workspace-row ${state.view === "pc" ? "is-active" : ""}" type="button">
                    <span>${icon("folder")}</span>
                    <div><strong>${PROJECTS.pc.name}</strong><small>${PROJECTS.pc.provider}</small></div>
                    <i id="wsDotPc" class="${state.view === "pc" && state.running ? "is-running" : ""}"></i>
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <footer class="sidebar-footer">
          <button class="new-workspace new-workspace--footer" type="button">
            <span>${icon("plus")}</span><strong>New workspace</strong>${icon("arrow")}
          </button>
        </footer>
      </aside>

      <main class="app-main" id="appMain">
        <header class="app-topbar">
          <div class="app-topbar__title"><small id="topbarDevice">${project.device}</small><strong>${project.name}</strong></div>
          <div class="app-topbar__providers">
            <span class="topbar-provider">${project.logo}<strong>${project.provider}</strong><i aria-label="Ready"></i></span>
          </div>
          <button class="icon-button" type="button" aria-label="Refresh">${icon("refresh")}</button>
        </header>

        <section class="chat-workspace" aria-label="${project.name} chat">
          <div class="chat-scroll" id="chatScroll">
            <div class="chat-column">
              <header class="chat-context">
                <div><h1>${project.name}</h1><p>${project.device} · ${project.path}</p></div>
                <div class="chat-context__actions">
                  <span class="session-status is-${state.status}" id="sessionStatus"><i></i>${statusLabel(state.status)}</span>
                  <button class="delete-conversation" type="button" aria-label="Delete conversation">${icon("trash")}</button>
                </div>
              </header>
              <div class="chat-items" id="chatItems"></div>
            </div>
          </div>

          <footer class="chat-composer-shell">
            <div class="chat-composer-stack">
              <form class="chat-composer" id="chatForm">
                <textarea id="chatInput" rows="1" placeholder="Message ${project.provider}…" aria-label="Message ${project.provider}"></textarea>
                <div class="chat-composer__footer">
                  <div class="chat-composer__meta">
                    <span class="composer-access-note">Workspace access</span>
                    <span class="composer-run-state ${state.running ? "is-running" : ""}" id="runState">${
                      state.running ? runningNote : `Runs on ${project.ownerLabel}`
                    }</span>
                    ${state.running ? "" : '<span class="composer-paste-hint">Type / for commands · Paste a screenshot</span>'}
                  </div>
                  <button type="submit" aria-label="Send message" id="sendButton" disabled>${icon("arrow")}</button>
                </div>
              </form>
            </div>
          </footer>
        </section>
      </main>
    </div>`;
}

/* ---------- chat item builders (markup mirrors main.js) ---------- */

const chatItems = () => document.querySelector("#chatItems");

function scrollChat() {
  const scroller = document.querySelector("#chatScroll");
  if (scroller) scroller.scrollTop = scroller.scrollHeight;
}

function addHTML(html) {
  chatItems().insertAdjacentHTML("beforeend", html);
  scrollChat();
  return chatItems().lastElementChild;
}

function emptyState() {
  const project = PROJECTS[state.view];
  chatItems().innerHTML = `
    <div class="chat-empty">
      <span>${project.logo}</span>
      <h2>Start working with ${project.provider}</h2>
      <p>Send a message to begin a resumable session in this workspace.</p>
    </div>`;
}

function userBubble(text) {
  document.querySelector(".chat-empty")?.remove();
  return addHTML(`
    <article class="chat-message chat-message--user">
      <div class="chat-bubble"><div class="chat-bubble__text">${text}</div></div>
    </article>`);
}

function agentLoader() {
  const project = PROJECTS[state.view];
  return addHTML(`
    <div class="agent-loader" role="status">
      <span class="chat-message__avatar">${project.logo}</span>
      <div>
        <strong>${project.provider}</strong>
        <span class="agent-loader__line"><i></i><i></i><i></i><small>${project.provider} is thinking…</small></span>
      </div>
    </div>`);
}

function toolGroup(title) {
  return addHTML(`
    <details class="tool-group is-running" open>
      <summary class="tool-group__summary">
        <span class="tool-group__status" aria-hidden="true"></span>
        <span><strong>${title}</strong><small class="tg-detail">Working…</small></span>
        ${icon("chevron")}
      </summary>
      <div class="tool-group__steps"></div>
    </details>`);
}

function toolStep(group, title, status) {
  const steps = group.querySelector(".tool-group__steps");
  steps.insertAdjacentHTML(
    "beforeend",
    `<div class="tool-step is-${status}">
      <span class="tool-step__status" aria-hidden="true"></span>
      <strong>${title}</strong>
      <small>${statusLabel(status)}</small>
    </div>`,
  );
  scrollChat();
  return steps.lastElementChild;
}

function setStep(step, status) {
  step.className = `tool-step is-${status}`;
  step.querySelector("small").textContent = statusLabel(status);
}

function assistantMessage(html) {
  const project = PROJECTS[state.view];
  return addHTML(`
    <article class="chat-message chat-message--assistant">
      <span class="chat-message__avatar">${project.logo}</span>
      <div><strong>${project.provider}</strong><div class="chat-copy markdown-body">${html}</div></div>
    </article>`);
}

/* ---------- status helpers ---------- */

function setSessionStatus(status) {
  const project = PROJECTS[state.view];
  state.status = status;
  state.running = status === "running";
  const pill = document.querySelector("#sessionStatus");
  pill.className = `session-status is-${status}`;
  pill.innerHTML = `<i></i>${statusLabel(status)}`;

  const activeDot = document.querySelector(state.view === "mac" ? "#wsDotMac" : "#wsDotPc");
  if (activeDot) activeDot.className = state.running ? "is-running" : "";

  const runState = document.querySelector("#runState");
  if (state.running) {
    runState.classList.add("is-running");
    runState.textContent = `${project.provider} is working on ${project.ownerLabel}`;
  } else {
    runState.classList.remove("is-running");
    runState.textContent = `Runs on ${project.ownerLabel}`;
  }
}

/* ---------- driver primitives ---------- */

const PARAMS = new URLSearchParams(location.search);
const EMBED = PARAMS.has("embed");
if (EMBED) document.body.classList.add("is-embed");
const SPEED = Number(PARAMS.get("speed") || 1);
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms / SPEED));

async function typeText(text, cps = 30) {
  const input = document.querySelector("#chatInput");
  const send = document.querySelector("#sendButton");
  input.focus();
  for (let i = 0; i <= text.length; i++) {
    input.value = text.slice(0, i);
    if (i > 0) send.removeAttribute("disabled");
    await sleep(1000 / cps + (Math.random() * 40 - 20));
  }
}

async function sendMessage() {
  const input = document.querySelector("#chatInput");
  const text = input.value;
  input.value = "";
  document.querySelector("#sendButton").setAttribute("disabled", "");
  userBubble(text);
  await sleep(150);
}

async function switchDevice(view) {
  const badge = document.querySelector("#deviceBadge");
  document.querySelector("#deviceBadgeLabel").textContent =
    view === "pc" ? "One click — now driving the Work PC" : "Back on the MacBook";
  badge.hidden = false;

  const main = document.querySelector("#appMain");
  main.classList.add("is-switching");
  await sleep(250);

  state.view = view;
  state.status = "idle";
  state.running = false;
  renderShell();
  emptyState();
  scrollChat();

  await sleep(2600);
  badge.hidden = true;
}

/* ---------- the choreography ---------- */

async function run() {
  renderShell();
  emptyState();
  await sleep(1600);

  // Beat 1 — first task from the MacBook
  await typeText("Fix the webhook retry bug in the payments flow, then run the test suite.");
  await sleep(350);
  await sendMessage();
  setSessionStatus("running");

  const loader = agentLoader();
  await sleep(2100);
  loader.remove();

  const group = toolGroup("Working in payments-api");
  const s1 = toolStep(group, "Read webhook/retry.ts", "running");
  await sleep(1100);
  setStep(s1, "completed");
  const s2 = toolStep(group, "Edit retry.ts — refresh stale event ID", "running");
  await sleep(1500);
  setStep(s2, "completed");
  const s3 = toolStep(group, "npm test — payments suite", "running");
  await sleep(2300);
  setStep(s3, "completed");
  group.className = "tool-group is-completed";
  group.querySelector(".tg-detail").textContent = "3 steps · completed";
  group.removeAttribute("open");
  await sleep(500);

  assistantMessage(
    "<p>Found it — the retry handler was reusing a <strong>stale event ID</strong>, so every retry was rejected as a duplicate. I refreshed the ID on each attempt and re-ran the suite: <strong>all 24 payment tests pass</strong>.</p>",
  );
  await sleep(900);
  setSessionStatus("completed");
  await sleep(2400);

  // Beat 2 — open the Work PC's own project, without leaving the Mac
  await switchDevice("pc");
  await sleep(600);

  // Beat 3 — a different project, a different agent, on the other machine
  await typeText("Generate the weekly inventory report and email it to ops.");
  await sleep(300);
  await sendMessage();
  setSessionStatus("running");

  const loader2 = agentLoader();
  await sleep(1900);
  loader2.remove();

  const group2 = toolGroup("Working in inventory-sync");
  const t1 = toolStep(group2, "Run report generator — 4,182 SKUs", "running");
  await sleep(1700);
  setStep(t1, "completed");
  const t2 = toolStep(group2, "Send report to ops@company.com", "running");
  await sleep(1600);
  setStep(t2, "completed");
  group2.className = "tool-group is-completed";
  group2.querySelector(".tg-detail").textContent = "2 steps · completed";
  group2.removeAttribute("open");
  await sleep(400);

  assistantMessage(
    "<p>Weekly report generated for <strong>4,182 SKUs</strong> and emailed to ops. This ran on your Work PC — you never left the MacBook.</p>",
  );
  await sleep(800);
  setSessionStatus("completed");

  // Hold the final frame for the recording tail
  await sleep(4000);
  if (PARAMS.has("loop") || EMBED) {
    state.view = "mac";
    state.status = "idle";
    state.running = false;
    run();
  }
}

run();
