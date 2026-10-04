import { NextResponse } from 'next/server';
import { configuration, requireSession, syncInstallation } from '../../../lib/server';
export async function GET(request: Request): Promise<Response> {
  try {
    const config = configuration(),
      session = await requireSession(),
      installationId = Number(new URL(request.url).searchParams.get('installation_id'));
    if (!Number.isSafeInteger(installationId) || installationId <= 0)
      return Response.json({ error: '安装编号无效。' }, { status: 400 });
    await syncInstallation(session, installationId);
    return NextResponse.redirect(`${config.appUrl}/repositories`);
  } catch {
    return Response.json(
      { error: '无法验证此安装。请先登录、确认安装属于当前账号，然后在仓库页同步。' },
      { status: 403 },
    );
  }
}
