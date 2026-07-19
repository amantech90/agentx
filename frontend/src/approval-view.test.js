import assert from "node:assert/strict";
import test from "node:test";

import { approvalCardModel } from "./approval-view.js";

test("approval cards offer a decision only while the request is pending", () => {
  const pending = approvalCardModel({
    kind: "approval",
    status: "pending",
    approval: { kind: "command", command: "go test ./..." },
  });
  assert.deepEqual(pending, {
    status: "pending",
    pending: true,
    kind: "Command",
    detail: "go test ./...",
    result: "Waiting for you",
  });

  const approved = approvalCardModel({ status: "approved", approval: { kind: "command", command: "go test ./..." } });
  assert.equal(approved.pending, false);
  assert.equal(approved.result, "Allowed once");
});
