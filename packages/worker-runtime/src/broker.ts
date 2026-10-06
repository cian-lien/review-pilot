import { randomUUID } from 'node:crypto';
import { setTimeout as delay } from 'node:timers/promises';
import { connect, type ChannelModel, type ConfirmChannel } from 'amqplib';
import { RestClient } from './client';
import { processDelivery } from './consumer';
import { safeLog } from '@reviewpilot/observability';
const queue = 'reviewpilot.review.v1',
  quarantine = 'reviewpilot.quarantine.v1';
export async function runReviewWorker(signal: AbortSignal): Promise<void> {
  const client = new RestClient(
    process.env.INTERNAL_API_URL!,
    process.env.REVIEW_WORKER_SERVICE_TOKEN!,
    'review-' + randomUUID(),
  );
  while (!signal.aborted) {
    let connection: ChannelModel | undefined;
    let channel: ConfirmChannel | undefined;
    let abort: (() => void) | undefined;
    try {
      connection = await connect(process.env.RABBITMQ_URL!, { timeout: 5000 });
      const closed = new Promise<void>((resolve) => {
        connection!.once('close', resolve);
        abort = () => {
          void connection?.close().catch(() => {});
          resolve();
        };
        signal.addEventListener('abort', abort, { once: true });
      });
      connection.on('error', () => {
        safeLog({ service: 'worker-review', code: 'BROKER_CONNECTION_INTERRUPTED' });
      });
      channel = await connection.createConfirmChannel();
      channel.on('error', () => {
        safeLog({ service: 'worker-review', code: 'BROKER_CHANNEL_INTERRUPTED' });
      });
      channel.once('close', () => {
        void connection?.close().catch(() => {});
      });
      await channel.assertExchange('reviewpilot.tasks.v1', 'direct', { durable: true });
      await channel.assertQueue(queue, { durable: true });
      await channel.bindQueue(queue, 'reviewpilot.tasks.v1', 'connection-check');
      await channel.assertQueue(quarantine, { durable: true });
      await channel.prefetch(1);
      // Serial processing follows prefetch=1; no automatic acknowledgement.
      await channel.consume(
        queue,
        (message) => {
          if (!message) return;
          void (async () => {
            let input: unknown;
            try {
              if (message.content.length > 16384) throw new Error();
              input = JSON.parse(message.content.toString('utf8'));
            } catch {
              input = null;
            }
            let disposition = await processDelivery(input, client);
            for (
              let attempt = 0;
              disposition === 'retry' && attempt < 2 && !signal.aborted;
              attempt++
            ) {
              await delay(1000 * 2 ** attempt, undefined, { signal }).catch(() => {});
              if (!signal.aborted) disposition = await processDelivery(input, client);
            }
            if (signal.aborted) return;
            if (disposition === 'retry') {
              // A closed channel redelivers after reconnect backoff; never hot nack/requeue.
              await connection!.close();
              return;
            }
            if (disposition === 'quarantine') {
              // Deliberately do not retain invalid raw input, secrets or arbitrary source.
              let returned = false;
              const onReturn = () => {
                returned = true;
              };
              channel!.on('return', onReturn);
              try {
                await new Promise<void>((resolve, reject) => {
                  channel!.sendToQueue(
                    quarantine,
                    Buffer.from('{"code":"INVALID_TASK_MESSAGE"}'),
                    { persistent: true, mandatory: true, contentType: 'application/json' },
                    (error) => (error ? reject(new Error('QUARANTINE_FAILED')) : resolve()),
                  );
                });
                if (returned) throw new Error('QUARANTINE_UNROUTABLE');
              } finally {
                channel!.off('return', onReturn);
              }
            }
            channel!.ack(message);
          })().catch(() => {
            safeLog({ service: 'worker-review', code: 'DELIVERY_UNCONFIRMED' });
            void connection?.close().catch(() => {});
          });
        },
        { noAck: false },
      );
      safeLog({ service: 'worker-review', code: 'REVIEW_WORKER_READY' });
      await closed;
    } catch {
      safeLog({ service: 'worker-review', code: 'REVIEW_WORKER_UNAVAILABLE' });
    } finally {
      if (abort) signal.removeEventListener('abort', abort);
      await channel?.close().catch(() => {});
      await connection?.close().catch(() => {});
    }
    if (!signal.aborted) await delay(5000, undefined, { signal }).catch(() => {});
  }
}
