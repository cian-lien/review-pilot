import { expect, it } from 'vitest';
import { readBoundedBytes } from '../src/read-bounded';
it('cancels an oversized UTF8 response without materializing it', async () => {
  let cancelled = false;
  const response = new Response(
    new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('汉'.repeat(100)));
      },
      cancel() {
        cancelled = true;
      },
    }),
  );
  await expect(readBoundedBytes(response, 256, new AbortController().signal)).rejects.toThrow(
    'RESPONSE_TOO_LARGE',
  );
  expect(cancelled).toBe(true);
});
it('rejects aborted reads and preserves exact bytes within the limit', async () => {
  const controller = new AbortController();
  controller.abort();
  await expect(readBoundedBytes(new Response('x'), 10, controller.signal)).rejects.toThrow();
  expect(
    new TextDecoder().decode(
      await readBoundedBytes(new Response('汉'), 3, new AbortController().signal),
    ),
  ).toBe('汉');
});
