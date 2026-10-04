import { cookies } from 'next/headers';
import { NextResponse } from 'next/server';
import { exchangeOAuthCode, matchesOAuthState, sealToken } from '@reviewpilot/github';
import { consumeOAuthAttempt, createSession } from '@reviewpilot/persistence';
import {
  configuration,
  database,
  github,
  cookieOptions,
  OAUTH_COOKIE,
  SESSION_COOKIE,
} from '../../../../lib/server';
export async function GET(request: Request): Promise<Response> {
  try {
    const config = configuration(),
      params = new URL(request.url).searchParams,
      store = await cookies();
    const state = params.get('state'),
      code = params.get('code'),
      expected = store.get(OAUTH_COOKIE)?.value;
    if (!state || !code || !expected || !matchesOAuthState(expected, state))
      return Response.json({ error: '登录状态无效，请重新登录。' }, { status: 400 });
    const verifier = await consumeOAuthAttempt(database(), state),
      token = await exchangeOAuthCode(config, code, verifier);
    const user = await github().user(token.accessToken);
    const session = await createSession(
      database(),
      user,
      sealToken(token.accessToken, config.sessionSecret),
      token.expiresIn,
    );
    const response = NextResponse.redirect(`${config.appUrl}/repositories`);
    response.cookies.set(SESSION_COOKIE, session, cookieOptions(token.expiresIn));
    response.cookies.set(OAUTH_COOKIE, '', cookieOptions(0));
    return response;
  } catch {
    return Response.json({ error: 'GitHub 登录未完成，请重新登录。' }, { status: 400 });
  }
}
