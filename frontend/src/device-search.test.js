import test from "node:test";
import assert from "node:assert/strict";

import { createDeviceSearch } from "./device-search.js";

test("stops a nearby-device search after 30 seconds and allows retry", () => {
  const scheduled = [];
  const states = [];
  const search = createDeviceSearch({
    durationMs: 30_000,
    schedule(callback, delay) {
      scheduled.push({ callback, delay });
      return scheduled.length;
    },
    cancel() {},
    onChange(status, remainingSeconds) {
      states.push([status, remainingSeconds]);
    },
  });

  search.start();
  assert.equal(search.status(), "searching");
  assert.equal(search.remainingSeconds(), 30);
  assert.equal(scheduled[0].delay, 1_000);

  scheduled.shift().callback();
  assert.equal(search.remainingSeconds(), 29);

  for (let second = 29; second > 0; second -= 1) {
    scheduled.shift().callback();
  }
  assert.equal(search.status(), "finished");
  assert.equal(search.remainingSeconds(), 0);

  search.retry();
  assert.equal(search.status(), "searching");
  assert.equal(search.remainingSeconds(), 30);
  assert.deepEqual(states.slice(0, 2), [["searching", 30], ["searching", 29]]);
  assert.deepEqual(states.slice(-2), [["finished", 0], ["searching", 30]]);
});
