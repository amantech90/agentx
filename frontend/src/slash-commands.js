const commands = [
  {
    name: "review",
    title: "Review changes",
    description: "Find bugs, regressions, and missing tests",
    kind: "prompt",
  },
  {
    name: "test",
    title: "Run tests",
    description: "Run the relevant suite and diagnose failures",
    kind: "prompt",
  },
  {
    name: "explain",
    title: "Explain",
    description: "Explain a file, feature, or architecture",
    kind: "prompt",
  },
  {
    name: "fix",
    title: "Fix an issue",
    description: "Investigate a problem and implement the fix",
    kind: "prompt",
  },
  {
    name: "diff",
    title: "Summarize changes",
    description: "Inspect and explain the current diff",
    kind: "prompt",
  },
  {
    name: "init",
    title: "Initialize instructions",
    description: "Create the provider's project guidance file",
    kind: "prompt",
  },
  {
    name: "clear",
    title: "Clear conversation",
    description: "Start fresh without changing project files",
    kind: "action",
  },
  {
    name: "status",
    title: "Workspace status",
    description: "Show where this session is running",
    kind: "action",
  },
  {
    name: "plan",
    title: "Plan mode",
    description: "Let Claude inspect and plan without editing",
    kind: "action",
    providers: ["claude"],
  },
  {
    name: "ask",
    title: "Ask for approval",
    description: "Show Allow once or Deny for sensitive actions",
    kind: "action",
    providers: ["claude"],
  },
  {
    name: "auto",
    title: "Automatic approvals",
    description: "Let Claude review permission requests automatically",
    kind: "action",
    providers: ["claude"],
  },
  {
    name: "accept-edits",
    title: "Accept edits",
    description: "Allow Claude to apply workspace edits",
    kind: "action",
    providers: ["claude"],
  },
];

function commandAvailable(command, providerID) {
  return !command.providers || command.providers.includes(providerID);
}

export function commandMatches(content, providerID) {
  const match = String(content || "").match(/^\/([a-z-]*)$/i);
  if (!match) return [];
  const query = match[1].toLowerCase();
  return commands.filter(
    (command) => commandAvailable(command, providerID) && command.name.startsWith(query),
  );
}

export function parseSlashCommand(content, providerID) {
  const match = String(content || "").trim().match(/^\/([a-z-]+)(?:\s+([\s\S]*))?$/i);
  if (!match) return null;
  const name = match[1].toLowerCase();
  const command = commands.find((candidate) => candidate.name === name && commandAvailable(candidate, providerID));
  if (!command) return null;
  return {
    command: name,
    arguments: String(match[2] || "").trim(),
    kind: command.kind,
  };
}

export function expandPromptCommand(name, argumentsText, providerID) {
  const detail = String(argumentsText || "").trim();
  switch (name) {
    case "review":
      return `Review the current workspace changes. Prioritize correctness, regressions, security issues, and missing tests. Report findings first, ordered by severity.${detail ? ` Focus especially on: ${detail}.` : ""}`;
    case "test":
      return `Run the relevant tests for the current workspace. Diagnose failures and fix them when safe.${detail ? ` Test scope: ${detail}.` : ""}`;
    case "explain":
      return detail
        ? `Explain ${detail} in the context of this workspace. Include the important files and execution flow.`
        : "Explain this workspace's architecture, important files, and execution flow.";
    case "fix":
      return detail
        ? `Investigate and fix this issue in the current workspace: ${detail}`
        : "Inspect the current workspace, identify the most important actionable defect, and fix it safely.";
    case "diff":
      return `Inspect the current uncommitted changes and give a concise, useful summary of what changed and any risks.${detail ? ` Focus on: ${detail}.` : ""}`;
    case "init":
      return providerID === "claude"
        ? "Analyze this workspace and create or improve CLAUDE.md with concise project commands, architecture, and repository conventions."
        : "Analyze this workspace and create or improve AGENTS.md with concise project commands, architecture, and repository conventions.";
    default:
      return "";
  }
}
