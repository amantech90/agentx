import test from "node:test";
import assert from "node:assert/strict";
import {
  configuredProvidersForDevice,
  findOpenedWorkspace,
  workspaceEntries,
  workspaceKey,
} from "./workspace-routing.js";

const state = {
  device: { id: "mac", name: "Mac" },
  providers: [{ id: "codex", installed: true, supported: true }],
  selectedProviderIds: ["codex"],
  workspaces: [{ id: "same", name: "Local", updatedAt: "1" }],
  remoteDevices: [{
    device: { id: "windows", name: "Windows" },
    online: true,
    providers: [
      { id: "claude", installed: true, supported: true },
      { id: "codex", installed: false, supported: true },
    ],
    selectedProviderIds: ["claude", "codex"],
    workspaces: [{ id: "same", name: "Remote", updatedAt: "2" }],
  }],
};

test("workspace identity includes the owning device", () => {
  assert.notEqual(workspaceKey("mac", "same"), workspaceKey("windows", "same"));
  assert.deepEqual(workspaceEntries(state).map((entry) => entry.key), ["mac:same", "windows:same"]);
});

test("remote project creation only offers providers installed on that device", () => {
  assert.deepEqual(configuredProvidersForDevice(state, "windows").map((provider) => provider.id), ["claude"]);
});

test("a newly changed remote workspace is selected with its owner", () => {
  const previous = new Map([
    ["mac:same", "1"],
    ["windows:same", "1"],
  ]);
  assert.deepEqual(findOpenedWorkspace(previous, state), {
    deviceID: "windows",
    workspace: state.remoteDevices[0].workspaces[0],
  });
});
