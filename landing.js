const header = document.querySelector("#siteHeader");
const scrollProgress = document.querySelector("#scrollProgress");
const menuButton = document.querySelector("#menuButton");
const mobileNav = document.querySelector("#mobileNav");
const downloadToast = document.querySelector("#downloadToast");
const toastTitle = document.querySelector("#toastTitle");
const toastMessage = document.querySelector("#toastMessage");
const closeToast = document.querySelector("#closeToast");

let toastTimer;
let scrollFrameRequested = false;

function detectPlatform() {
  const platform = (
    navigator.userAgentData?.platform ||
    navigator.platform ||
    navigator.userAgent ||
    ""
  ).toLowerCase();

  if (platform.includes("mac")) return "macos";
  if (platform.includes("win")) return "windows";
  if (platform.includes("linux")) return "linux";
  return "auto";
}

function platformLabel(platform) {
  if (platform === "macos") return "Download for macOS";
  if (platform === "windows") return "Download for Windows";
  return "Download Agent X";
}

function updatePlatformButtons() {
  const platform = detectPlatform();
  document.querySelectorAll('[data-download="auto"] .button__platform').forEach((label) => {
    label.textContent = platformLabel(platform);
  });
}

function hideDownloadToast() {
  if (downloadToast) downloadToast.hidden = true;
}

function showDownloadToast(platform) {
  const selectedPlatform = platform === "auto" ? detectPlatform() : platform;
  const readablePlatform =
    selectedPlatform === "macos"
      ? "macOS"
      : selectedPlatform === "windows"
        ? "Windows"
        : "macOS and Windows";

  if (toastTitle) toastTitle.textContent = `Agent X for ${readablePlatform}`;
  if (toastMessage) {
    toastMessage.textContent =
      selectedPlatform === "linux"
        ? "Agent X is available for macOS and Windows."
        : "Your download is starting…";
  }

  if (downloadToast) downloadToast.hidden = false;
  window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(hideDownloadToast, 5200);
}

function setMenuOpen(open) {
  if (!menuButton || !mobileNav) return;
  menuButton.setAttribute("aria-expanded", String(open));
  menuButton.setAttribute("aria-label", open ? "Close navigation" : "Open navigation");
  mobileNav.hidden = !open;
}

function updateScrollUI() {
  const scrollTop = window.scrollY;
  const scrollableHeight = document.documentElement.scrollHeight - window.innerHeight;
  const progress = scrollableHeight > 0 ? (scrollTop / scrollableHeight) * 100 : 0;

  header?.classList.toggle("is-scrolled", scrollTop > 24);
  if (scrollProgress) scrollProgress.style.width = `${Math.min(progress, 100)}%`;
  scrollFrameRequested = false;
}

function requestScrollUpdate() {
  if (scrollFrameRequested) return;
  scrollFrameRequested = true;
  window.requestAnimationFrame(updateScrollUI);
}

function revealSections() {
  const elements = document.querySelectorAll(".reveal-on-scroll");

  if (!('IntersectionObserver' in window)) {
    elements.forEach((element) => element.classList.add("is-visible"));
    return;
  }

  const revealObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        entry.target.classList.add("is-visible");
        revealObserver.unobserve(entry.target);
      });
    },
    { rootMargin: "0px 0px 14%", threshold: 0.04 },
  );

  elements.forEach((element) => revealObserver.observe(element));
}

function trackCurrentSection() {
  if (!('IntersectionObserver' in window)) return;

  const links = [...document.querySelectorAll('.desktop-nav a[href^="#"]')];
  const sections = links
    .map((link) => document.querySelector(link.getAttribute("href")))
    .filter(Boolean);

  const sectionObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        links.forEach((link) => {
          link.classList.toggle("is-current", link.getAttribute("href") === `#${entry.target.id}`);
        });
      });
    },
    { rootMargin: "-25% 0px -60%", threshold: 0.01 },
  );

  sections.forEach((section) => sectionObserver.observe(section));
}

const RELEASES_URL = "https://github.com/amantech90/agentx/releases/latest";
const DIRECT_DOWNLOADS = {
  macos: `${RELEASES_URL}/download/AgentX-macOS.zip`,
  windows: `${RELEASES_URL}/download/AgentX-Windows.exe`,
};

document.querySelectorAll("[data-download]").forEach((button) => {
  button.addEventListener("click", () => {
    const platform = button.dataset.download || "auto";
    const resolved = platform === "auto" ? detectPlatform() : platform;
    showDownloadToast(platform);
    const directURL = DIRECT_DOWNLOADS[resolved];
    if (directURL) window.location.href = directURL;
    else window.open(RELEASES_URL, "_blank", "noopener");
  });
});

menuButton?.addEventListener("click", () => {
  setMenuOpen(menuButton.getAttribute("aria-expanded") !== "true");
});

mobileNav?.querySelectorAll("a").forEach((link) => {
  link.addEventListener("click", () => setMenuOpen(false));
});

closeToast?.addEventListener("click", hideDownloadToast);
window.addEventListener("scroll", requestScrollUpdate, { passive: true });
window.addEventListener("resize", () => {
  if (window.innerWidth > 900) setMenuOpen(false);
});

updatePlatformButtons();
updateScrollUI();
revealSections();
trackCurrentSection();
