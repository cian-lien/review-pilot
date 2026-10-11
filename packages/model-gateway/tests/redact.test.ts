import { expect, it } from 'vitest';
import { redactReview } from '../src/redact';
it('uses the same line-preserving redaction for generation and verification', () => {
  const input =
    'const api_key = "sk-123456789012345678901234";\n// token=ghp_123456789012345678901234\nreturn value;';
  const output = redactReview(input);
  expect(output).not.toContain('sk-1234');
  expect(output).not.toContain('ghp_1234');
  expect(output.split('\n')).toHaveLength(input.split('\n').length);
  expect(redactReview(output)).toBe(output);
});
