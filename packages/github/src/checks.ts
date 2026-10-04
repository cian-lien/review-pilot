import { z } from 'zod';
import { GitHubClient, repositoryPath } from './client';

export const CONNECTION_CHECK_NAME = 'ReviewPilot · 连接检查';
const checkSchema = z.object({
  id: z.number().int().positive().safe(),
  external_id: z.string().nullable(),
  name: z.string(),
});
export async function findConnectionCheck(
  api: GitHubClient,
  token: string,
  repository: string,
  headSha: string,
  runId: string,
): Promise<number | null> {
  for (let page = 1; page <= 50; page++) {
    const result = z
      .object({ check_runs: z.array(checkSchema) })
      .parse(
        await api.request(
          `/repos/${repositoryPath(repository)}/commits/${headSha}/check-runs?filter=all&check_name=${encodeURIComponent(CONNECTION_CHECK_NAME)}&per_page=100&page=${page}`,
          token,
        ),
      );
    const found = result.check_runs.find(
      (check) =>
        check.external_id === `reviewpilot:${runId}` && check.name === CONNECTION_CHECK_NAME,
    );
    if (found) return found.id;
    if (result.check_runs.length < 100) return null;
  }
  throw new Error('Check reconciliation pagination limit exceeded');
}
export async function updateConnectionCheck(
  api: GitHubClient,
  token: string,
  input: {
    repository: string;
    headSha: string;
    runId: string;
    checkId: number | null;
    appUrl: string;
    completed: boolean;
  },
  guard: () => Promise<void>,
): Promise<number> {
  // Reconcile a prior successful write whose response was lost, rather than blindly creating another Check.
  const existing =
    input.checkId ??
    (await findConnectionCheck(api, token, input.repository, input.headSha, input.runId));
  await guard();
  const output = {
    title: input.completed ? '连接检查完成（未进行 AI 审查）' : '正在验证 GitHub 接入',
    summary: '此 Check 仅验证事件、任务与 GitHub 写入链路。尚未分析代码，不能代表代码正确或安全。',
  };
  const body = {
    name: CONNECTION_CHECK_NAME,
    external_id: `reviewpilot:${input.runId}`,
    details_url: `${input.appUrl}/repositories`,
    status: input.completed ? 'completed' : 'in_progress',
    ...(input.completed ? { conclusion: 'neutral', completed_at: new Date().toISOString() } : {}),
    output,
    ...(existing ? {} : { head_sha: input.headSha }),
  };
  const result = checkSchema.parse(
    await api.request(
      `/repos/${repositoryPath(input.repository)}/check-runs${existing ? `/${existing}` : ''}`,
      token,
      existing ? 'PATCH' : 'POST',
      body,
    ),
  );
  if (result.external_id !== `reviewpilot:${input.runId}` || result.name !== CONNECTION_CHECK_NAME)
    throw new Error('Check identity mismatch');
  return result.id;
}
