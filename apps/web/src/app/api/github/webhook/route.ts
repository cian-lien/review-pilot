import { verifyWebhookSignature } from '@reviewpilot/github';
import { acceptDelivery } from '@reviewpilot/persistence';
import { database } from '../../../../lib/server';
export const runtime = 'nodejs';
export const dynamic = 'force-dynamic';
const MAX_BODY_BYTES = 2 * 1024 * 1024;
export async function POST(request: Request): Promise<Response> {
  const secret = process.env.GITHUB_WEBHOOK_SECRET;
  if (!secret) return Response.json({ error: 'Webhook 未配置。' }, { status: 503 });
  const reader = request.body?.getReader();
  if (!reader) return Response.json({ error: '缺少请求体。' }, { status: 400 });
  const chunks: Buffer[] = [];
  let length = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      length += value.byteLength;
      if (length > MAX_BODY_BYTES) {
        await reader.cancel();
        return Response.json({ error: '请求体过大。' }, { status: 413 });
      }
      chunks.push(Buffer.from(value));
    }
    const raw = Buffer.concat(chunks);
    if (
      !verifyWebhookSignature(raw, request.headers.get('x-hub-signature-256') ?? undefined, secret)
    )
      return Response.json({ error: '签名无效。' }, { status: 401 });
    const deliveryId = request.headers.get('x-github-delivery'),
      event = request.headers.get('x-github-event');
    if (!deliveryId || !event)
      return Response.json({ error: '事件元数据不完整。' }, { status: 400 });
    const result = await acceptDelivery(
      database(),
      deliveryId,
      event,
      raw,
      process.env.REVIEW_PIPELINE_VERSION ?? 'foundation-v1',
    );
    return Response.json(result, { status: 202 });
  } catch (error) {
    const invalid =
      error instanceof SyntaxError || (error instanceof Error && ['ZodError'].includes(error.name));
    return Response.json(
      { error: invalid ? '事件格式无效。' : '事件未接收，请稍后重试。' },
      { status: invalid ? 400 : 503 },
    );
  } finally {
    reader.releaseLock();
  }
}
