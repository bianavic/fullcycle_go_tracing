package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"service-b/internal/domain"
	"service-b/internal/usecase"
)

const (
	incomingTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	incomingSpanID  = "00f067aa0ba902b7"
	traceparent     = "00-" + incomingTraceID + "-" + incomingSpanID + "-01"
)

// spyUseCase records the span context that reaches the application layer.
type spyUseCase struct{ gotSC trace.SpanContext }

func (s *spyUseCase) Execute(ctx context.Context, _ domain.CEP) (usecase.Output, error) {
	s.gotSC = trace.SpanContextFromContext(ctx)
	return usecase.Output{City: "X"}, nil
}

func tracedRouter(t *testing.T, uc WeatherUseCase, logOut *bytes.Buffer) (http.Handler, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	logger := discardLogger()
	if logOut != nil {
		logger = slog.New(slog.NewJSONHandler(logOut, nil))
	}
	return NewRouter(NewHandler(uc, discardLogger()), logger,
		otelhttp.WithTracerProvider(tp),
		otelhttp.WithPropagators(propagation.TraceContext{}),
	), rec
}

func tracedPost(h http.Handler, body string, withParent bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/weather", strings.NewReader(body))
	if withParent {
		req.Header.Set("traceparent", traceparent)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRouter_ServerSpanContinuesIncomingTrace(t *testing.T) {
	spy := &spyUseCase{}
	h, rec := tracedRouter(t, spy, nil)

	tracedPost(h, `{"cep":"01310100"}`, true)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1 server span", len(spans))
	}
	span := spans[0]
	if span.Name() != "POST /weather" {
		t.Errorf("span name = %q, want %q", span.Name(), "POST /weather")
	}
	if span.SpanKind() != trace.SpanKindServer {
		t.Errorf("span kind = %v, want server", span.SpanKind())
	}
	if got := span.SpanContext().TraceID().String(); got != incomingTraceID {
		t.Errorf("trace id = %s, want the caller's %s", got, incomingTraceID)
	}
	if got := span.Parent().SpanID().String(); got != incomingSpanID {
		t.Errorf("parent span id = %s, want the caller's %s", got, incomingSpanID)
	}
	if spy.gotSC.SpanID() != span.SpanContext().SpanID() {
		t.Error("the use case must receive the server span in its context, so child spans attach to it")
	}
}

func TestRouter_StartsNewTraceWithoutIncomingContext(t *testing.T) {
	h, rec := tracedRouter(t, &spyUseCase{}, nil)

	tracedPost(h, `{"cep":"01310100"}`, false)

	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Parent().IsValid() {
		t.Fatalf("want one root span, got %d (parent valid=%v)", len(spans), len(spans) == 1 && spans[0].Parent().IsValid())
	}
}

func TestRouter_RecordsResponseStatusOnSpan(t *testing.T) {
	h, rec := tracedRouter(t, &spyUseCase{}, nil)

	tracedPost(h, `{"cep":"123"}`, true)

	var got int64
	for _, kv := range rec.Ended()[0].Attributes() {
		if kv.Key == "http.response.status_code" {
			got = kv.Value.AsInt64()
		}
	}
	if got != http.StatusUnprocessableEntity {
		t.Errorf("http.response.status_code = %d, want 422", got)
	}
}

func TestRouter_HealthChecksAreNotTraced(t *testing.T) {
	h, rec := tracedRouter(t, &spyUseCase{}, nil)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if n := len(rec.Ended()); n != 0 {
		t.Errorf("got %d spans for /healthz, want none", n)
	}
}

// Span names come from the route pattern, never from the raw path, so scanners
// hitting random URLs cannot create unbounded span-name cardinality.
func TestRouter_UnmatchedPathsShareOneSpanName(t *testing.T) {
	h, rec := tracedRouter(t, &spyUseCase{}, nil)

	for _, path := range []string{"/admin", "/.env", "/a/b/c"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
	}

	names := map[string]bool{}
	for _, s := range rec.Ended() {
		names[s.Name()] = true
		if strings.Contains(s.Name(), "/admin") || strings.Contains(s.Name(), ".env") {
			t.Errorf("span name %q leaks the raw path", s.Name())
		}
	}
	if len(names) != 1 {
		t.Errorf("got span names %v, want a single shared name", names)
	}
}

func TestRouter_LogLineCarriesTheServerSpanTraceID(t *testing.T) {
	var logs bytes.Buffer
	h, _ := tracedRouter(t, &spyUseCase{}, &logs)

	tracedPost(h, `{"cep":"01310100"}`, true)

	var line map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &line); err != nil {
		t.Fatalf("log is not a single JSON line: %v (%q)", err, logs.String())
	}
	if line["trace_id"] != incomingTraceID {
		t.Errorf("trace_id = %v, want %s", line["trace_id"], incomingTraceID)
	}
}
