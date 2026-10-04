import {
  createCipheriv,
  createDecipheriv,
  createHash,
  randomBytes,
  timingSafeEqual,
} from 'node:crypto';
export function createOAuthAttempt(): { state: string; verifier: string; challenge: string } {
  const state = randomBytes(32).toString('base64url'),
    verifier = randomBytes(32).toString('base64url');
  return { state, verifier, challenge: createHash('sha256').update(verifier).digest('base64url') };
}
export function matchesOAuthState(expected: string, received: string | undefined): boolean {
  if (!received || expected.length !== received.length) return false;
  const left = Buffer.from(expected),
    right = Buffer.from(received);
  return left.length === right.length && timingSafeEqual(left, right);
}
function secretKey(secret: string): Buffer {
  const key = Buffer.from(secret, 'base64');
  if (key.length !== 32 || key.toString('base64') !== secret)
    throw new Error('SESSION_SECRET must encode 32 random bytes as base64');
  return key;
}
export function sealToken(token: string, secret: string): string {
  const iv = randomBytes(12),
    cipher = createCipheriv('aes-256-gcm', secretKey(secret), iv);
  const encrypted = Buffer.concat([cipher.update(token, 'utf8'), cipher.final()]);
  return [iv, cipher.getAuthTag(), encrypted].map((part) => part.toString('base64url')).join('.');
}
export function openToken(sealed: string, secret: string): string {
  const parts = sealed.split('.');
  if (parts.length !== 3) throw new Error('Invalid sealed credential');
  const [iv, tag, encrypted] = parts.map((part) => Buffer.from(part, 'base64url'));
  if (!iv || iv.length !== 12 || !tag || tag.length !== 16 || !encrypted)
    throw new Error('Invalid sealed credential');
  const decipher = createDecipheriv('aes-256-gcm', secretKey(secret), iv);
  decipher.setAuthTag(tag);
  return Buffer.concat([decipher.update(encrypted), decipher.final()]).toString('utf8');
}
export function requireSameOrigin(request: Request, appUrl: string): void {
  if (request.headers.get('origin') !== new URL(appUrl).origin)
    throw new Error('Invalid request origin');
}
