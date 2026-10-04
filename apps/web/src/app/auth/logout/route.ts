import { NextResponse } from 'next/server';
import { requireSameOrigin } from '@reviewpilot/github';
import { deleteSession } from '@reviewpilot/persistence';
import {
  configuration,
  database,
  getSession,
  cookieOptions,
  SESSION_COOKIE,
} from '../../../lib/server';
export async function POST(request: Request): Promise<Response> {
  try {
    const config = configuration();
    requireSameOrigin(request, config.appUrl);
    const session = await getSession();
    if (session) await deleteSession(database(), session.token);
    const response = NextResponse.redirect(`${config.appUrl}/repositories`, 303);
    response.cookies.set(SESSION_COOKIE, '', cookieOptions(0));
    return response;
  } catch {
    return Response.json({ error: '无法完成退出。' }, { status: 403 });
  }
}
