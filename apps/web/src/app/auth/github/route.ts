import { NextResponse } from 'next/server';
import { createOAuthAttempt } from '@reviewpilot/github';
import { saveOAuthAttempt } from '@reviewpilot/persistence';
import { configuration, database, cookieOptions, OAUTH_COOKIE } from '../../../lib/server';
export async function GET(): Promise<Response> {
  try {
    const config = configuration(),
      attempt = createOAuthAttempt();
    await saveOAuthAttempt(database(), attempt.state, attempt.verifier);
    const url = new URL('https://github.com/login/oauth/authorize');
    url.search = new URLSearchParams({
      client_id: config.clientId,
      redirect_uri: `${config.appUrl}/auth/github/callback`,
      state: attempt.state,
      code_challenge: attempt.challenge,
      code_challenge_method: 'S256',
    }).toString();
    const response = NextResponse.redirect(url);
    response.cookies.set(OAUTH_COOKIE, attempt.state, cookieOptions(600));
    return response;
  } catch {
    return Response.json({ error: 'GitHub 登录暂不可用，请检查服务端配置。' }, { status: 503 });
  }
}
