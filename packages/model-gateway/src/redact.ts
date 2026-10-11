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
export function redactStructured(value: unknown): unknown {
  if (typeof value === 'string') return redactReview(value);
  if (Array.isArray(value)) return value.map(redactStructured);
  if (value && typeof value === 'object')
    return Object.fromEntries(
      Object.entries(value).map(([key, child]) => [key, redactStructured(child)]),
    );
  return value;
}
