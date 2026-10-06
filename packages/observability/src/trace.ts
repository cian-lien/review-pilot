import { context, trace, metrics } from '@opentelemetry/api';
import { logs } from '@opentelemetry/api-logs';
import { W3CTraceContextPropagator } from '@opentelemetry/core';
import { resourceFromAttributes } from '@opentelemetry/resources';
import { NodeSDK } from '@opentelemetry/sdk-node';
import { BatchSpanProcessor } from '@opentelemetry/sdk-trace-node';
import { AlwaysOnSampler, ParentBasedSampler } from '@opentelemetry/sdk-trace-node';
import { writeFileSync } from 'node:fs';
import { BatchLogRecordProcessor } from '@opentelemetry/sdk-logs';
import { PeriodicExportingMetricReader } from '@opentelemetry/sdk-metrics';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { OTLPLogExporter } from '@opentelemetry/exporter-logs-otlp-http';
import { OTLPMetricExporter } from '@opentelemetry/exporter-metrics-otlp-http';
import { safeFields, safeLog, setLogSink, type LogFields } from './log';
const propagator = new W3CTraceContextPropagator();
const getter = {
  keys: (carrier: Record<string, string>) => Object.keys(carrier),
  get: (carrier: Record<string, string>, key: string) => carrier[key],
};
export async function setupObservability(service: string): Promise<() => Promise<void>> {
  const configured = process.env.OTEL_EXPORTER_OTLP_ENDPOINT;
  if (!configured) return async () => {};
  const address = new URL(configured);
  if (
    !['http:', 'https:'].includes(address.protocol) ||
    address.username ||
    address.password ||
    address.search ||
    address.hash ||
    address.pathname !== '/'
  )
    throw new Error('INVALID_TELEMETRY_CONFIG');
  const endpoint = address.origin;
  const batch = {
    maxQueueSize: 512,
    maxExportBatchSize: 64,
    scheduledDelayMillis: 1000,
    exportTimeoutMillis: 1000,
  };
  const sdk = new NodeSDK({
    resource: resourceFromAttributes({
      'service.name': service,
      'service.version': 'foundation-v1',
    }),
    autoDetectResources: false,
    sampler: new ParentBasedSampler({ root: new AlwaysOnSampler() }),
    instrumentations: [],
    textMapPropagator: propagator,
    spanProcessors: [
      new BatchSpanProcessor(
        new OTLPTraceExporter({
          url: endpoint + '/v1/traces',
          timeoutMillis: 1000,
          concurrencyLimit: 1,
        }),
        batch,
      ),
    ],
    logRecordProcessors: [
      new BatchLogRecordProcessor({
        exporter: new OTLPLogExporter({
          url: endpoint + '/v1/logs',
          timeoutMillis: 1000,
          concurrencyLimit: 1,
        }),
        ...batch,
      }),
    ],
    metricReader: new PeriodicExportingMetricReader({
      exporter: new OTLPMetricExporter({
        url: endpoint + '/v1/metrics',
        timeoutMillis: 1000,
        concurrencyLimit: 1,
      }),
      exportIntervalMillis: 15000,
      exportTimeoutMillis: 1000,
    }),
  });
  sdk.start();
  const logger = logs.getLogger('reviewpilot');
  setLogSink((fields) =>
    logger.emit({ body: fields.code ?? 'EVENT', attributes: fields, context: context.active() }),
  );
  return async () => {
    setLogSink(undefined);
    await sdk.shutdown().catch(() => {});
  };
}
export function traceHeaders(): Record<string, string> {
  const headers: Record<string, string> = {};
  propagator.inject(context.active(), headers, {
    set: (carrier, key, value) => {
      if (key === 'traceparent') carrier[key] = value;
    },
  });
  return headers;
}
export async function inTaskTrace<T>(
  envelope: { runId: string; taskId: string; traceparent?: string },
  work: () => Promise<T>,
): Promise<T> {
  const parent =
    envelope.traceparent && /^00-[a-f0-9]{32}-[a-f0-9]{16}-[a-f0-9]{2}$/.test(envelope.traceparent)
      ? propagator.extract(context.active(), { traceparent: envelope.traceparent }, getter)
      : context.active();
  const fields: LogFields = {
    service: 'worker-review',
    runId: envelope.runId,
    taskId: envelope.taskId,
    stage: 'worker.consume',
  };
  return trace
    .getTracer('reviewpilot')
    .startActiveSpan('worker.consume', { attributes: safeFields(fields) }, parent, async (span) => {
      const started = performance.now();
      try {
        return await work();
      } finally {
        const durationMs = performance.now() - started;
        safeLog({
          ...fields,
          traceId: span.spanContext().traceId,
          code: 'DELIVERY_PROCESSED',
          durationMs,
        });
        metrics
          .getMeter('reviewpilot')
          .createHistogram('reviewpilot.stage.duration', { unit: 'ms' })
          .record(durationMs, { stage: 'worker.consume' });
        span.end();
      }
    });
}
export function workerHeartbeat(service: string): void {
  if (process.env.HEARTBEAT_FILE) {
    try {
      writeFileSync(process.env.HEARTBEAT_FILE, 'alive\n', { mode: 0o600 });
    } catch {
      /* health check reports unavailable */
    }
  }
  metrics
    .getMeter('reviewpilot')
    .createGauge('reviewpilot.worker.heartbeat.timestamp', { unit: 's' })
    .record(Date.now() / 1000, { service });
}
