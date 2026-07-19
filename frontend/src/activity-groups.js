export function groupChatItems(items = []) {
  const groups = [];
  for (const item of items) {
    if (item.kind !== "activity") {
      groups.push({ kind: "item", item });
      continue;
    }

    const previous = groups[groups.length - 1];
    if (previous?.kind === "activity-group") {
      previous.items.push(item);
    } else {
      groups.push({ kind: "activity-group", items: [item] });
    }
  }
  return groups;
}

export function normalizeActivityStatus(status) {
  if (status === "running" || status === "in_progress") return "running";
  if (status === "failed" || status === "error" || status === "cancelled" || status === "canceled") return "failed";
  return "completed";
}

export function summarizeActivityGroup(items = []) {
  let current = items[items.length - 1] || {};
  for (let index = items.length - 1; index >= 0; index -= 1) {
    if (normalizeActivityStatus(items[index].status) === "running") {
      current = items[index];
      break;
    }
  }
  const failed = items.some((item) => normalizeActivityStatus(item.status) === "failed");
  const running = items.some((item) => normalizeActivityStatus(item.status) === "running");
  const status = running ? "running" : failed ? "failed" : "completed";

  if (running) {
    return {
      status,
      title: current.title || "Working",
      detail: `Running · ${items.length} ${items.length === 1 ? "action" : "actions"}`,
    };
  }
  if (items.length === 1) {
    return { status, title: current.title || "Agent activity", detail: failed ? "Failed" : "Completed" };
  }
  return {
    status,
    title: `${items.length} actions ${failed ? "finished with an error" : "completed"}`,
    detail: current.title ? `Last: ${current.title}` : "",
  };
}
