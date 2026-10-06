package observability

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

type Fields struct {
	RequestID, RunID, TaskID, Stage, Code string
	Attempt                               int
	DurationMs                            float64
}
type Runtime struct {
	Service  string
	Logger   *slog.Logger
	traces   *sdktrace.TracerProvider
	logs     *sdklog.LoggerProvider
	meters   *sdkmetric.MeterProvider
	duration metric.Float64Histogram
	events   metric.Int64Counter
}
type runtimeKey struct{}

var current atomic.Pointer[Runtime]
var uuid = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var hexID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var parentPattern = regexp.MustCompile(`^00-[a-f0-9]{32}-[a-f0-9]{16}-[a-f0-9]{2}$`)
var stagePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
var codePattern = regexp.MustCompile(`^[A-Z_]{1,64}$`)

func Setup(ctx context.Context, service string) (func(context.Context) error, error) {
	r, err := NewRuntime(ctx, service, os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		return func(context.Context) error { return nil }, err
	}
	current.Store(r)
	slog.SetDefault(r.Logger)
	return r.Close, nil
}
func Current() *Runtime { return current.Load() }
func FromContext(ctx context.Context) *Runtime {
	if r, ok := ctx.Value(runtimeKey{}).(*Runtime); ok {
		return r
	}
	return Current()
}
func NewRuntime(ctx context.Context, service, endpoint string) (*Runtime, error) {
	if service != "api" && service != "dispatcher" {
		return nil, errors.New("INVALID_TELEMETRY_SERVICE")
	}
	res := resource.NewSchemaless(attribute.String("service.name", service), attribute.String("service.version", "foundation-v1"))
	r := &Runtime{Service: service}
	// Export errors are constant stdout-only events, never recursively exported logs.
	diagnostic := slog.New(&safeHandler{outputs: []slog.Handler{slog.NewJSONHandler(os.Stdout, nil)}, service: service})
	var lastDiagnostic atomic.Int64
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {
		now := time.Now().Unix()
		previous := lastDiagnostic.Load()
		if now-previous >= 15 && lastDiagnostic.CompareAndSwap(previous, now) {
			diagnostic.Warn("TELEMETRY_UNAVAILABLE")
		}
	}))
	traceOptions := []sdktrace.TracerProviderOption{sdktrace.WithResource(res), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1)))}
	logOptions := []sdklog.LoggerProviderOption{sdklog.WithResource(res)}
	metricOptions := []sdkmetric.Option{sdkmetric.WithResource(res)}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return nil, errors.New("INVALID_TELEMETRY_CONFIG")
		}
		traceExporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"), otlptracehttp.WithTimeout(time.Second), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}))
		if err != nil {
			return nil, errors.New("TELEMETRY_SETUP_FAILED")
		}
		logExporter, err := otlploghttp.New(ctx, otlploghttp.WithEndpointURL(endpoint+"/v1/logs"), otlploghttp.WithTimeout(time.Second), otlploghttp.WithRetry(otlploghttp.RetryConfig{Enabled: false}))
		if err != nil {
			return nil, errors.New("TELEMETRY_SETUP_FAILED")
		}
		metricExporter, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint+"/v1/metrics"), otlpmetrichttp.WithTimeout(time.Second), otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{Enabled: false}))
		if err != nil {
			return nil, errors.New("TELEMETRY_SETUP_FAILED")
		}
		traceOptions = append(traceOptions, sdktrace.WithBatcher(traceExporter, sdktrace.WithMaxQueueSize(512), sdktrace.WithMaxExportBatchSize(64), sdktrace.WithBatchTimeout(time.Second), sdktrace.WithExportTimeout(time.Second)))
		logOptions = append(logOptions, sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter, sdklog.WithMaxQueueSize(512), sdklog.WithExportMaxBatchSize(64), sdklog.WithExportInterval(time.Second), sdklog.WithExportTimeout(time.Second))))
		metricOptions = append(metricOptions, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(15*time.Second), sdkmetric.WithTimeout(time.Second))))
	}
	r.traces = sdktrace.NewTracerProvider(traceOptions...)
	r.logs = sdklog.NewLoggerProvider(logOptions...)
	r.meters = sdkmetric.NewMeterProvider(metricOptions...)
	r.duration, _ = r.meters.Meter("reviewpilot").Float64Histogram("reviewpilot.stage.duration", metric.WithUnit("ms"))
	r.events, _ = r.meters.Meter("reviewpilot").Int64Counter("reviewpilot.events")
	r.Logger = slog.New(&safeHandler{outputs: []slog.Handler{slog.NewJSONHandler(os.Stdout, nil), otelslog.NewHandler("reviewpilot", otelslog.WithLoggerProvider(r.logs))}, service: service})
	return r, nil
}
func (r *Runtime) Close(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return errors.Join(r.traces.Shutdown(bounded), r.logs.Shutdown(bounded), r.meters.Shutdown(bounded))
}
func (r *Runtime) Flush(ctx context.Context) error {
	return errors.Join(r.traces.ForceFlush(ctx), r.logs.ForceFlush(ctx), r.meters.ForceFlush(ctx))
}
func Extract(ctx context.Context, parent string) context.Context {
	if !parentPattern.MatchString(parent) {
		return ctx
	}
	return (propagation.TraceContext{}).Extract(ctx, propagation.MapCarrier{"traceparent": parent})
}
func Traceparent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	(propagation.TraceContext{}).Inject(ctx, carrier)
	return carrier.Get("traceparent")
}
func fieldsAttrs(f Fields) []attribute.KeyValue {
	out := []attribute.KeyValue{}
	for key, value := range map[string]string{"requestId": f.RequestID, "runId": f.RunID, "taskId": f.TaskID, "stage": f.Stage, "code": f.Code} {
		valid := (key == "requestId" && hexID.MatchString(value)) || ((key == "runId" || key == "taskId") && uuid.MatchString(value)) || (key == "stage" && stagePattern.MatchString(value)) || (key == "code" && codePattern.MatchString(value))
		if valid {
			out = append(out, attribute.String(key, value))
		}
	}
	if f.Attempt > 0 && f.Attempt <= 1000000 {
		out = append(out, attribute.Int("attempt", f.Attempt))
	}
	return out
}
func (r *Runtime) Start(ctx context.Context, stage string, fields Fields) (context.Context, trace.Span) {
	if r == nil {
		return ctx, trace.SpanFromContext(context.Background())
	}
	if !stagePattern.MatchString(stage) {
		stage = "unknown"
	}
	fields.Stage = stage
	ctx, span := r.traces.Tracer("reviewpilot").Start(context.WithValue(ctx, runtimeKey{}, r), stage, trace.WithAttributes(fieldsAttrs(fields)...))
	return ctx, &timedSpan{Span: span, r: r, ctx: ctx, stage: stage, started: time.Now()}
}

type timedSpan struct {
	trace.Span
	r       *Runtime
	ctx     context.Context
	stage   string
	started time.Time
}

func (s *timedSpan) End(options ...trace.SpanEndOption) {
	s.r.duration.Record(s.ctx, float64(time.Since(s.started).Microseconds())/1000, metric.WithAttributes(attribute.String("stage", s.stage)))
	s.r.events.Add(s.ctx, 1, metric.WithAttributes(attribute.String("stage", s.stage)))
	s.Span.End(options...)
}
func (r *Runtime) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := Extract(c.Request.Context(), c.GetHeader("traceparent"))
		ctx, span := r.Start(ctx, "http.request", Fields{RequestID: c.GetString("requestId")})
		defer span.End()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
		span.SetAttributes(attribute.String("http.route", c.FullPath()), attribute.Int("http.response.status_code", c.Writer.Status()))
		if c.Writer.Status() >= 500 {
			r.events.Add(ctx, 1, metric.WithAttributes(attribute.String("stage", "http.failure")))
		}
	}
}
func (r *Runtime) RecordState(ctx context.Context, db *gorm.DB) {
	if r == nil || db == nil {
		return
	}
	queries := map[string]string{
		"reviewpilot.jobs.pending":         "SELECT count(*) FROM jobs WHERE status IN ('queued','running','result_ready')",
		"reviewpilot.jobs.failed":          "SELECT count(*) FROM jobs WHERE status='failed'",
		"reviewpilot.jobs.oldest.age":      "SELECT COALESCE(GREATEST(EXTRACT(EPOCH FROM clock_timestamp()-min(available_at)),0),0) FROM jobs WHERE status='queued'",
		"reviewpilot.outbox.pending":       "SELECT count(*) FROM outbox_events WHERE sent_at IS NULL",
		"reviewpilot.outbox.oldest.age":    "SELECT COALESCE(GREATEST(EXTRACT(EPOCH FROM clock_timestamp()-min(available_at)),0),0) FROM outbox_events WHERE sent_at IS NULL",
		"reviewpilot.publications.pending": "SELECT count(*) FROM publication_requests WHERE state IN ('pending','running')",
	}
	for name, query := range queries {
		var value float64
		if db.WithContext(ctx).Raw(query).Scan(&value).Error != nil {
			continue
		}
		gauge, _ := r.meters.Meter("reviewpilot").Float64Gauge(name)
		gauge.Record(ctx, value)
	}
	gauge, _ := r.meters.Meter("reviewpilot").Float64Gauge("reviewpilot.dispatcher.heartbeat.timestamp", metric.WithUnit("s"))
	gauge.Record(ctx, float64(time.Now().Unix()))
}

// The bridge and stdout receive the same allowlisted record, never raw errors or request data.
type safeHandler struct {
	outputs []slog.Handler
	service string
	attrs   []slog.Attr
	grouped bool
}

func (h *safeHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *safeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &clone
}
func (h *safeHandler) WithGroup(string) slog.Handler {
	clone := *h
	clone.grouped = true
	return &clone
}
func (h *safeHandler) Handle(ctx context.Context, record slog.Record) error {
	code := record.Message
	if !codePattern.MatchString(code) {
		code = "EVENT"
	}
	safe := slog.NewRecord(record.Time, record.Level, code, 0)
	safe.AddAttrs(slog.String("service", h.service))
	accept := func(a slog.Attr) {
		if h.grouped {
			return
		}
		value := a.Value.Resolve()
		valid := false
		if value.Kind() == slog.KindString {
			s := value.String()
			valid = ((a.Key == "runId" || a.Key == "taskId") && uuid.MatchString(s)) || ((a.Key == "requestId" || a.Key == "traceId") && hexID.MatchString(s)) || (a.Key == "stage" && stagePattern.MatchString(s)) || (a.Key == "code" && codePattern.MatchString(s))
		}
		if value.Kind() == slog.KindInt64 {
			valid = (a.Key == "attempt" || a.Key == "durationMs") && value.Int64() >= 0 && value.Int64() <= 1000000000
		}
		if valid {
			safe.AddAttrs(slog.Attr{Key: a.Key, Value: value})
		}
	}
	for _, a := range h.attrs {
		accept(a)
	}
	record.Attrs(func(a slog.Attr) bool { accept(a); return true })
	if id := trace.SpanContextFromContext(ctx).TraceID(); id.IsValid() {
		safe.AddAttrs(slog.String("traceId", id.String()))
	}
	for _, output := range h.outputs {
		_ = output.Handle(ctx, safe)
	}
	return nil
}
