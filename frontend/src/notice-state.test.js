import assert from "node:assert/strict";
import test from "node:test";

import { createNoticeState } from "./notice-state.js";

test("keeps a send error available across an immediate shell rerender", () => {
  let expire;
  const notices = createNoticeState({
    schedule(callback) {
      expire = callback;
      return 1;
    },
    cancel() {},
  });

  notices.show("Windows rejected the message", true);
  assert.deepEqual(notices.current(), { message: "Windows rejected the message", isError: true });

  // renderShell reads the same state after submitChatMessage's finally block.
  assert.deepEqual(notices.current(), { message: "Windows rejected the message", isError: true });

  expire();
  assert.equal(notices.current(), null);
});
