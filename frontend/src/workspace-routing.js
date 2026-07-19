export function workspaceKey(deviceID, workspaceID) {
  return `${String(deviceID || "")}:${String(workspaceID || "")}`;
}

export function remoteDeviceByID(state, deviceID) {
  return (state?.remoteDevices || []).find((item) => item?.device?.id === deviceID) || null;
}

export function deviceContext(state, deviceID) {
  if (!state) return null;
  if (!deviceID || deviceID === state.device?.id) {
    return {
      device: state.device,
      online: true,
      providers: state.providers || [],
      selectedProviderIds: state.selectedProviderIds || [],
      workspaces: state.workspaces || [],
      sessions: [],
      local: true,
    };
  }
  return remoteDeviceByID(state, deviceID);
}

export function workspacesForDevice(state, deviceID) {
  return deviceContext(state, deviceID)?.workspaces || [];
}

export function providersForDevice(state, deviceID) {
  return deviceContext(state, deviceID)?.providers || [];
}

export function configuredProvidersForDevice(state, deviceID) {
  const context = deviceContext(state, deviceID);
  const selected = new Set(context?.selectedProviderIds || []);
  return (context?.providers || []).filter(
    (provider) => selected.has(provider.id) && provider.installed && provider.supported && !provider.comingSoon,
  );
}

export function workspaceEntries(state) {
  if (!state) return [];
  const entries = (state.workspaces || []).map((workspace) => ({
    deviceID: state.device.id,
    workspace,
    key: workspaceKey(state.device.id, workspace.id),
  }));
  for (const remote of state.remoteDevices || []) {
    for (const workspace of remote.workspaces || []) {
      entries.push({
        deviceID: remote.device.id,
        workspace,
        key: workspaceKey(remote.device.id, workspace.id),
      });
    }
  }
  return entries;
}

export function findOpenedWorkspace(previous, state) {
  for (const entry of workspaceEntries(state)) {
    if (!previous.has(entry.key) || previous.get(entry.key) !== entry.workspace.updatedAt) {
      return { deviceID: entry.deviceID, workspace: entry.workspace };
    }
  }
  return null;
}
