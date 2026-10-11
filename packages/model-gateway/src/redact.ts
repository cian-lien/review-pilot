export function redactReview(text: string): string {
  return text
    .replace(
      /(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{16,}|AKIA[A-Z0-9]{16})/g,
      '[REDACTED]',
    )
    .replace(
      /((?:api[_-]?key|secret|password|access[_-]?token)\s*[:=]\s*["']?)[^\s"';,]{8,}/gi,
      '$1[REDACTED]',
    );
}
