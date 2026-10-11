import type { EvidenceRef, ReviewChangedFile } from '@reviewpilot/contracts';
export interface ParsedReviewDiff {
  locations: { path: string; sha: string; line: number }[];
  skipped: { path: string; reasonCode: string }[];
}
export function safeRepositoryPath(path: string): boolean {
  return (
    path.length > 0 &&
    path.length <= 1024 &&
    !/[\u0000-\u001f\\:]/.test(path) &&
    !path.startsWith('/') &&
    path.split('/').every((part) => part !== '' && part !== '.' && part !== '..')
  );
}
function lines(text: string): string[] {
  const result = text.replaceAll('\r\n', '\n').split('\n');
  if (result.at(-1) === '') result.pop();
  return result;
}
function parseFile(file: ReviewChangedFile): ParsedReviewDiff['locations'] {
  if (file.head.state !== 'available' || file.head.text === null)
    throw new Error('INSUFFICIENT_CONTEXT');
  if (file.status !== 'added' && (file.base.state !== 'available' || file.base.text === null))
    throw new Error('INSUFFICIENT_CONTEXT');
  const base = lines(file.base.text ?? '');
  const head = lines(file.head.text);
  const result: ParsedReviewDiff['locations'] = [];
  let oldLine = 0,
    newLine = 0,
    oldRemaining = 0,
    newRemaining = 0,
    started = false,
    added = 0,
    removed = 0;
  for (const line of lines(file.patch ?? '')) {
    const header = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(?:.*)$/.exec(line);
    if (header) {
      if (oldRemaining || newRemaining) throw new Error('PATCH_MISMATCH');
      oldLine = Number(header[1]);
      newLine = Number(header[3]);
      oldRemaining = Number(header[2] ?? 1);
      newRemaining = Number(header[4] ?? 1);
      started = true;
      continue;
    }
    if (line === '\\ No newline at end of file') continue;
    if (!started) throw new Error('PATCH_MISMATCH');
    const code = line.slice(1);
    if (line.startsWith(' ') || line.startsWith('-')) {
      if (oldRemaining <= 0 || base[oldLine - 1] !== code) throw new Error('PATCH_MISMATCH');
      oldLine++;
      oldRemaining--;
      if (line.startsWith('-')) removed++;
    }
    if (line.startsWith(' ') || line.startsWith('+')) {
      if (newRemaining <= 0 || head[newLine - 1] !== code) throw new Error('PATCH_MISMATCH');
      if (line.startsWith('+')) {
        result.push({ path: file.path, sha: file.head.sha, line: newLine });
        added++;
      }
      newLine++;
      newRemaining--;
    }
    if (![' ', '-', '+'].includes(line[0] ?? '')) throw new Error('PATCH_MISMATCH');
  }
  if (
    !started ||
    oldRemaining ||
    newRemaining ||
    added !== file.additions ||
    removed !== file.deletions
  )
    throw new Error('PATCH_MISMATCH');
  return result;
}
export function parseDiff(files: ReviewChangedFile[]): ParsedReviewDiff {
  const result: ParsedReviewDiff = { locations: [], skipped: [] };
  for (const file of files) {
    let reason: string | undefined;
    if (
      !safeRepositoryPath(file.path) ||
      (file.previousPath !== null && !safeRepositoryPath(file.previousPath))
    )
      reason = 'INVALID_PATH';
    else if (file.status === 'removed') reason = 'DELETED_FILE';
    else if (file.patch === null) reason = 'MISSING_PATCH';
    else
      try {
        result.locations.push(...parseFile(file));
      } catch (error) {
        reason = error instanceof Error ? error.message : 'PATCH_MISMATCH';
      }
    if (reason) result.skipped.push({ path: file.path, reasonCode: reason });
  }
  return result;
}
export function validateDiffLocation(ref: EvidenceRef, diff: ParsedReviewDiff): boolean {
  return (
    Number.isSafeInteger(ref.startLine) &&
    Number.isSafeInteger(ref.endLine) &&
    ref.startLine > 0 &&
    ref.endLine >= ref.startLine &&
    diff.locations.some(
      (location) =>
        location.path === ref.path &&
        location.sha === ref.sha &&
        location.line >= ref.startLine &&
        location.line <= ref.endLine,
    )
  );
}
