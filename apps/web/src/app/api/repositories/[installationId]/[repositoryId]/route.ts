import { NextResponse } from 'next/server';
import { requireSameOrigin } from '@reviewpilot/github';
import { setRepositoryEnabled, synchronizeRepository } from '@reviewpilot/persistence';
import {
  configuration,
  database,
  github,
  requireSession,
  requireRepoAccess,
} from '../../../../../lib/server';
export async function POST(
  request: Request,
  context: { params: Promise<{ installationId: string; repositoryId: string }> },
): Promise<Response> {
  try {
    const config = configuration();
    requireSameOrigin(request, config.appUrl);
    const params = await context.params,
      tenant = {
        installationId: Number(params.installationId),
        repositoryId: Number(params.repositoryId),
      };
    if (Object.values(tenant).some((id) => !Number.isSafeInteger(id) || id <= 0))
      return Response.json({ error: '仓库编号无效。' }, { status: 400 });
    const body = await request.formData(),
      enabled = body.get('enabled');
    if (enabled !== 'true' && enabled !== 'false')
      return Response.json({ error: '启用参数无效。' }, { status: 400 });
    const session = await requireSession();
    await requireRepoAccess(tenant, 'manage');
    const repository = (
      await github().userInstallationRepositories(session.userToken, tenant.installationId)
    ).find((item) => item.id === tenant.repositoryId && item.public);
    if (!repository) return Response.json({ error: '此仓库不可访问。' }, { status: 403 });
    await synchronizeRepository(database(), {
      ...tenant,
      fullName: repository.fullName,
      public: repository.public,
    });
    await setRepositoryEnabled(database(), tenant, enabled === 'true');
    return NextResponse.redirect(`${config.appUrl}/repositories`, 303);
  } catch {
    return Response.json(
      { error: '只有具有 maintain/admin 权限的当前用户可以调整公共仓库。' },
      { status: 403 },
    );
  }
}
