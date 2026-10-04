import { NextResponse } from 'next/server';
import { requireSameOrigin } from '@reviewpilot/github';
import { configuration, github, requireSession, syncInstallation } from '../../../../lib/server';
export async function POST(request: Request): Promise<Response> {
  try {
    const config = configuration();
    requireSameOrigin(request, config.appUrl);
    const session = await requireSession(),
      installations = await github().userInstallations(session.userToken);
    for (const item of installations.filter(
      (item) => item.appId === config.appId && !item.suspended,
    ))
      await syncInstallation(session, item.id);
    return NextResponse.redirect(`${config.appUrl}/repositories`, 303);
  } catch {
    return Response.json({ error: '同步失败，请确认登录和 GitHub 安装权限。' }, { status: 403 });
  }
}
