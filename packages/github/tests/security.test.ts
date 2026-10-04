import { createHmac } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { verifyWebhookSignature } from '../src/webhook';
import {
  createOAuthAttempt,
  matchesOAuthState,
  sealToken,
  openToken,
  requireSameOrigin,
} from '../src/auth';
import { requireRepositoryAccess, type AccessGateway } from '../src/installation-access';

describe('S1 安全边界', () => {
  it('验签基于原始字节，拒绝改动和畸形签名', () => {
    const body = Buffer.from('{"text":"中文"}'),
      secret = 'test-only-secret';
    const signature = `sha256=${createHmac('sha256', secret).update(body).digest('hex')}`;
    expect(verifyWebhookSignature(body, signature, secret)).toBe(true);
    expect(
      verifyWebhookSignature(Buffer.concat([body, Buffer.from('\n')]), signature, secret),
    ).toBe(false);
    expect(verifyWebhookSignature(body, 'sha256=abc', secret)).toBe(false);
    expect(verifyWebhookSignature(body, signature, '')).toBe(false);
  });
  it('OAuth 使用随机 state/PKCE，会话凭证加密并拒绝篡改', () => {
    const attempt = createOAuthAttempt(),
      other = createOAuthAttempt(),
      key = Buffer.alloc(32, 7).toString('base64');
    expect(attempt.state).not.toBe(other.state);
    expect(attempt.challenge).not.toBe(attempt.verifier);
    expect(matchesOAuthState(attempt.state, attempt.state)).toBe(true);
    expect(matchesOAuthState(attempt.state, other.state)).toBe(false);
    expect(matchesOAuthState(attempt.state, undefined)).toBe(false);
    const sealed = sealToken('test-only-token', key);
    expect(sealed).not.toContain('test-only-token');
    expect(openToken(sealed, key)).toBe('test-only-token');
    expect(() => openToken(sealed, Buffer.alloc(32, 8).toString('base64'))).toThrow();
    expect(() =>
      requireSameOrigin(
        new Request('http://localhost:3000', { headers: { origin: 'https://evil.example' } }),
        'http://localhost:3000',
      ),
    ).toThrow();
  });
  it('安装归属、公共仓库、维护权限分别校验', async () => {
    const tenant = { installationId: 12, repositoryId: 34 };
    const api: AccessGateway = {
      userInstallations: async () => [{ id: 12, appId: 99, suspended: false }],
      installationRepositories: async () => [{ id: 34, fullName: 'owner/repo', public: true }],
      userRepository: async () => ({
        id: 34,
        fullName: 'owner/repo',
        public: true,
        permissions: { pull: true, maintain: false, admin: false },
      }),
    };
    await expect(requireRepositoryAccess(api, tenant, 'view', 99)).resolves.toMatchObject({
      maintainer: false,
    });
    await expect(requireRepositoryAccess(api, tenant, 'manage', 99)).rejects.toThrow();
    await expect(
      requireRepositoryAccess(api, { ...tenant, installationId: 999 }, 'view', 99),
    ).rejects.toThrow();
    await expect(
      requireRepositoryAccess(
        {
          ...api,
          userRepository: async () => ({
            id: 34,
            fullName: 'owner/repo',
            public: false,
            permissions: { pull: true, maintain: true, admin: true },
          }),
        },
        tenant,
        'manage',
        99,
      ),
    ).rejects.toThrow();
  });
});
