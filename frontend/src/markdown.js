import DOMPurify from "dompurify";
import { marked } from "marked";

const allowedTags = [
  "p", "br", "strong", "em", "del", "blockquote",
  "ul", "ol", "li", "h1", "h2", "h3", "h4", "h5", "h6",
  "code", "pre", "hr", "a",
  "table", "thead", "tbody", "tr", "th", "td",
];

const sanitizeOptions = {
  ALLOWED_TAGS: allowedTags,
  ALLOWED_ATTR: ["href", "title"],
  ALLOW_DATA_ATTR: false,
  ALLOWED_URI_REGEXP: /^https?:\/\//i,
  RETURN_TRUSTED_TYPE: false,
};

export function renderMarkdown(value = "", purifier = DOMPurify) {
  const source = String(value);
  const parsed = marked.parse(source, {
    async: false,
    breaks: true,
    gfm: true,
  });
  return purifier.sanitize(parsed, sanitizeOptions);
}
