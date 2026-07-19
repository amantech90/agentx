import test from "node:test";
import assert from "node:assert/strict";

import {
  commandMatches,
  expandPromptCommand,
  parseSlashCommand,
} from "./slash-commands.js";

test("shows and filters commands while the first token starts with slash", () => {
  assert.deepEqual(commandMatches("/re", "codex").map((command) => command.name), ["review"]);
  assert.equal(commandMatches("please /review", "codex").length, 0);
  assert.equal(commandMatches("/review security", "codex").length, 0);
});

test("only shows Claude permission commands in Claude workspaces", () => {
  assert.equal(commandMatches("/plan", "codex").length, 0);
  assert.deepEqual(commandMatches("/plan", "claude").map((command) => command.name), ["plan"]);
});

test("parses known commands and leaves custom provider commands untouched", () => {
  assert.deepEqual(parseSlashCommand("/review authentication", "codex"), {
    command: "review",
    arguments: "authentication",
    kind: "prompt",
  });
  assert.equal(parseSlashCommand("/my-project-skill", "claude"), null);
});

test("expands prompt commands into clear agent instructions", () => {
  const prompt = expandPromptCommand("review", "authentication", "codex");
  assert.match(prompt, /review/i);
  assert.match(prompt, /authentication/i);
});

test("recognises commands handled entirely by Agent X", () => {
  assert.equal(parseSlashCommand("/clear", "codex")?.kind, "action");
  assert.equal(parseSlashCommand("/status", "claude")?.kind, "action");
  assert.equal(parseSlashCommand("/accept-edits", "claude")?.kind, "action");
  assert.equal(parseSlashCommand("/ask", "claude")?.kind, "action");
});
