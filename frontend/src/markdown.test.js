import assert from "node:assert/strict";
import test from "node:test";
import createDOMPurify from "dompurify";
import { JSDOM } from "jsdom";
import { renderMarkdown } from "./markdown.js";

function purifier() {
  return createDOMPurify(new JSDOM("<!doctype html><html><body></body></html>").window);
}

test("renders common assistant Markdown", () => {
  const html = renderMarkdown(
    "## Result\n\n- one\n- two\n\n`go test ./...`\n\n```go\nfmt.Println(\"ok\")\n```",
    purifier(),
  );

  assert.match(html, /<h2>Result<\/h2>/);
  assert.match(html, /<ul>/);
  assert.match(html, /<code>go test \.\/\.\.\.<\/code>/);
  assert.match(html, /<pre><code>fmt\.Println/);
});

test("removes executable HTML and remote images from assistant Markdown", () => {
  const html = renderMarkdown(
    '<script>alert("x")</script>\n<img src="https://tracker.example/pixel" onerror="alert(1)">\n[bad](javascript:alert(1))',
    purifier(),
  );

  assert.doesNotMatch(html, /<script/i);
  assert.doesNotMatch(html, /<img/i);
  assert.doesNotMatch(html, /onerror/i);
  assert.doesNotMatch(html, /href=["']javascript:/i);
});
