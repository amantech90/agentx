const MAX_CLIPBOARD_BYTES = 24 * 1024 * 1024;
const MAX_SCREENSHOT_BYTES = 2 * 1024 * 1024;
const MAX_PREVIEW_BYTES = 256 * 1024;
const SOURCE_MAX_EDGE = 2048;
const PREVIEW_MAX_EDGE = 560;

export function clipboardScreenshot(clipboardData) {
  for (const item of Array.from(clipboardData?.items || [])) {
    if (item.kind === "file" && String(item.type || "").startsWith("image/")) {
      return item.getAsFile?.() || null;
    }
  }
  return null;
}

export function canSubmitComposer(content, screenshot) {
  return Boolean(String(content || "").trim() || screenshot?.data);
}

export async function prepareScreenshot(file) {
  if (!file || !String(file.type || "").startsWith("image/")) {
    throw new Error("Paste a PNG, JPEG, or WebP screenshot.");
  }
  if (file.size > MAX_CLIPBOARD_BYTES) {
    throw new Error("That screenshot is too large to paste.");
  }

  const sourceURL = await fileToDataURL(file);
  const image = await loadImage(sourceURL);
  const dataURL = renderWithinLimit(image, [
    [SOURCE_MAX_EDGE, 0.88],
    [1720, 0.84],
    [1440, 0.8],
    [1200, 0.76],
  ], MAX_SCREENSHOT_BYTES);
  const previewURL = renderImage(image, PREVIEW_MAX_EDGE, 0.76);
  if (dataURLBytes(previewURL) > MAX_PREVIEW_BYTES) {
    throw new Error("That screenshot preview could not be prepared.");
  }
  return {
    mediaType: "image/jpeg",
    data: dataURL.slice(dataURL.indexOf(",") + 1),
    previewData: previewURL.slice(previewURL.indexOf(",") + 1),
  };
}

function renderWithinLimit(image, attempts, maximumBytes) {
  for (const [edge, quality] of attempts) {
    const result = renderImage(image, edge, quality);
    if (dataURLBytes(result) <= maximumBytes) return result;
  }
  throw new Error("That screenshot is too detailed to send. Try capturing a smaller area.");
}

function dataURLBytes(value) {
  const base64 = String(value || "").split(",", 2)[1] || "";
  return Math.floor((base64.length * 3) / 4);
}

function fileToDataURL(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.addEventListener("load", () => resolve(String(reader.result || "")), { once: true });
    reader.addEventListener("error", () => reject(new Error("The pasted screenshot could not be read.")), { once: true });
    reader.readAsDataURL(file);
  });
}

function loadImage(source) {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.addEventListener("load", () => resolve(image), { once: true });
    image.addEventListener("error", () => reject(new Error("The pasted screenshot is not a valid image.")), { once: true });
    image.src = source;
  });
}

function renderImage(image, maximumEdge, quality) {
  const scale = Math.min(1, maximumEdge / Math.max(image.naturalWidth, image.naturalHeight));
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(image.naturalWidth * scale));
  canvas.height = Math.max(1, Math.round(image.naturalHeight * scale));
  const context = canvas.getContext("2d");
  context.fillStyle = "#ffffff";
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.drawImage(image, 0, 0, canvas.width, canvas.height);
  return canvas.toDataURL("image/jpeg", quality);
}
