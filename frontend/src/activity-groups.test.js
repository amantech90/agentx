import assert from "node:assert/strict";
import test from "node:test";

import { groupChatItems, summarizeActivityGroup } from "./activity-groups.js";

test("groups only consecutive activities", () => {
  const grouped = groupChatItems([
    { id: "read", kind: "activity" },
    { id: "bash", kind: "activity" },
    { id: "answer", kind: "message" },
    { id: "edit", kind: "activity" },
  ]);

  assert.equal(grouped.length, 3);
  assert.deepEqual(grouped[0].items.map((item) => item.id), ["read", "bash"]);
  assert.equal(grouped[1].item.id, "answer");
  assert.deepEqual(grouped[2].items.map((item) => item.id), ["edit"]);
});

test("keeps the current concrete action visible while a group is running", () => {
  const summary = summarizeActivityGroup([
    { title: "Read src/main.cpp", status: "completed" },
    { title: "Run platformio test", status: "running" },
  ]);

  assert.deepEqual(summary, {
    status: "running",
    title: "Run platformio test",
    detail: "Running · 2 actions",
  });
});

test("treats Codex in_progress as an actively running tool", () => {
  const summary = summarizeActivityGroup([
    { title: "Run npm test", status: "in_progress" },
  ]);

  assert.deepEqual(summary, {
    status: "running",
    title: "Run npm test",
    detail: "Running · 1 action",
  });
});
