import test from "node:test";
import assert from "node:assert/strict";

import { clipboardScreenshot, canSubmitComposer } from "./image-input.js";

test("finds an image file in clipboard items", () => {
  const image = { type: "image/png", size: 100 };
  const clipboard = {
    items: [
      { kind: "string", type: "text/plain", getAsFile: () => null },
      { kind: "file", type: "image/png", getAsFile: () => image },
    ],
  };

  assert.equal(clipboardScreenshot(clipboard), image);
});

test("does not treat ordinary pasted text as a screenshot", () => {
  const clipboard = {
    items: [{ kind: "string", type: "text/plain", getAsFile: () => null }],
  };

  assert.equal(clipboardScreenshot(clipboard), null);
});

test("allows an image-only message to be sent", () => {
  assert.equal(canSubmitComposer("", { mediaType: "image/jpeg", data: "abc" }), true);
  assert.equal(canSubmitComposer("", null), false);
  assert.equal(canSubmitComposer("Explain this", null), true);
});
