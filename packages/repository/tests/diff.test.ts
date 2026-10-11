import { expect, it } from 'vitest';
import type { ReviewChangedFile } from '@reviewpilot/contracts';
import { parseDiff, validateDiffLocation } from '../src/diff';
const file: ReviewChangedFile = {
  path: 'src/a.ts',
  previousPath: null,
  status: 'modified',
  additions: 1,
  deletions: 1,
  patch:
    '@@ -1,2 +1,2 @@\r\n const a = 1;\r\n-old();\r\n+newCall();\r\n\\ No newline at end of file',
  base: {
    state: 'available',
    sha: 'b'.repeat(40),
    contentHash: 'c'.repeat(64),
    text: 'const a = 1;\r\nold();',
    bytes: 25,
  },
  head: {
    state: 'available',
    sha: 'a'.repeat(40),
    contentHash: 'd'.repeat(64),
    text: 'const a = 1;\r\nnewCall();',
    bytes: 29,
  },
};
it('maps only added new-side lines across CRLF and missing trailing newline', () => {
  const diff = parseDiff([file]);
  expect(diff.locations).toEqual([{ path: 'src/a.ts', sha: 'a'.repeat(40), line: 2 }]);
  const ref = {
    evidenceId: 'x',
    path: file.path,
    sha: file.head.sha,
    startLine: 2,
    endLine: 2,
    contentHash: 'd'.repeat(64),
  };
  expect(validateDiffLocation(ref, diff)).toBe(true);
  expect(validateDiffLocation({ ...ref, startLine: 1, endLine: 1 }, diff)).toBe(false);
});
it.each([
  [{ ...file, patch: null }, 'MISSING_PATCH'],
  [{ ...file, path: '../escape.ts' }, 'INVALID_PATH'],
  [{ ...file, patch: file.patch?.replace('+newCall();', '+invented();') ?? '' }, 'PATCH_MISMATCH'],
  [
    { ...file, status: 'removed', head: { ...file.head, state: 'deleted', text: null } },
    'DELETED_FILE',
  ],
] as const)('refuses unavailable or fabricated anchors', (input, reason) => {
  const parsed = parseDiff([input]);
  expect(parsed.locations).toEqual([]);
  expect(parsed.skipped).toEqual([{ path: input.path, reasonCode: reason }]);
});
it('uses the new path when a rename includes changed lines', () => {
  expect(
    parseDiff([{ ...file, path: 'src/new.ts', previousPath: file.path, status: 'renamed' }])
      .locations[0]?.path,
  ).toBe('src/new.ts');
});
