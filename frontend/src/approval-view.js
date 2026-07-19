const terminalStatuses = new Set(["approved", "denied", "cancelled"]);

export function approvalCardModel(item = {}) {
  const approval = item.approval || {};
  const status = terminalStatuses.has(item.status) ? item.status : "pending";
  const kind = approval.kind === "command"
    ? "Command"
    : approval.kind === "file-change"
      ? "File change"
      : "Tool request";
  const detail = approval.command
    || (approval.paths || []).join("\n")
    || item.content
    || "Review this action before the agent continues.";
  return {
    status,
    pending: status === "pending",
    kind,
    detail,
    result: status === "approved" ? "Allowed once" : status === "denied" ? "Denied" : status === "cancelled" ? "Cancelled" : "Waiting for you",
  };
}
