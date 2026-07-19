function activeUserIndex(items) {
  const running = items.findIndex((item) => item.role === "user" && item.status === "running");
  if (running >= 0) return running;
  return items.findIndex((item) => item.role === "user" && item.status === "queued");
}

function isProviderProgress(item) {
	return item.role === "assistant" || item.kind === "activity" || item.kind === "approval" || item.kind === "error";
}

export function shouldShowInitialAgentLoader({ sending = false, status = "idle", items = [] } = {}) {
  if (sending) return true;
	if (status !== "running" && status !== "queued" && status !== "waiting") return false;

  const userIndex = activeUserIndex(items);
  if (userIndex < 0) return true;

  const activeUser = items[userIndex];
  if (activeUser.turnId) {
    return !items.some(
      (item, index) => index !== userIndex && item.turnId === activeUser.turnId && isProviderProgress(item),
    );
  }

  // Compatibility for sessions created before turn IDs were introduced.
  return !items.slice(userIndex + 1).some(isProviderProgress);
}
