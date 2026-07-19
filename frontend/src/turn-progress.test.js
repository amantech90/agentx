import assert from "node:assert/strict";
import test from "node:test";

import { shouldShowInitialAgentLoader } from "./turn-progress.js";

test("shows progress for a new turn even when an older tool was left running", () => {
  const show = shouldShowInitialAgentLoader({
    status: "running",
    items: [
      { role: "user", status: "completed", kind: "message" },
      { role: "system", status: "running", kind: "activity" },
      { role: "user", status: "running", kind: "message" },
    ],
  });

  assert.equal(show, true);
});

test("does not mistake output from the previous queued turn for current progress", () => {
  const show = shouldShowInitialAgentLoader({
    status: "running",
    items: [
      { id: "user-1", turnId: "turn-1", role: "user", status: "completed", kind: "message" },
      { id: "user-2", turnId: "turn-2", role: "user", status: "running", kind: "message" },
      { turnId: "turn-1", role: "assistant", kind: "message", content: "First turn finished." },
    ],
  });

  assert.equal(show, true);
});

test("does not bring the thinking loader back between tools", () => {
  const show = shouldShowInitialAgentLoader({
    status: "running",
    items: [
      { turnId: "turn-1", role: "user", status: "running", kind: "message" },
      { turnId: "turn-1", role: "assistant", kind: "message", content: "I will inspect it." },
      { turnId: "turn-1", role: "system", status: "completed", kind: "activity" },
    ],
  });

  assert.equal(show, false);
});

test("shows progress immediately while the message is being queued", () => {
  assert.equal(shouldShowInitialAgentLoader({ sending: true, status: "idle", items: [] }), true);
});

test("hides the initial loader as soon as the current turn starts a tool", () => {
  const show = shouldShowInitialAgentLoader({
    status: "running",
    items: [
      { turnId: "turn-1", role: "user", status: "running", kind: "message" },
      { turnId: "turn-1", role: "system", status: "running", kind: "activity" },
    ],
  });

  assert.equal(show, false);
});

test("hides the thinking loader while an approval card is waiting", () => {
  const show = shouldShowInitialAgentLoader({
    status: "waiting",
    items: [
      { turnId: "turn-1", role: "user", status: "running", kind: "message" },
      { turnId: "turn-1", role: "system", status: "pending", kind: "approval" },
    ],
  });

  assert.equal(show, false);
});
