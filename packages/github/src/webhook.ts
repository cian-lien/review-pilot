import { createHmac, timingSafeEqual } from 'node:crypto';
export function verifyWebhookSignature(
  body: Buffer,
  signature: string | undefined,
  secret: string,
): boolean {
  if (!secret || !signature || !/^sha256=[0-9a-f]{64}$/.test(signature)) return false;
  return timingSafeEqual(
    createHmac('sha256', secret).update(body).digest(),
    Buffer.from(signature.slice(7), 'hex'),
  );
}
