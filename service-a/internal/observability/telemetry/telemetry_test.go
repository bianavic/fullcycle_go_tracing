package telemetry

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// capture is a fake OTLP/HTTP collector that records what the exporter sends.
type capture struct {
	mu    sync.Mutex
	paths []string
	body  []byte
}

func (c *capture) handler(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.paths = append(c.paths, r.URL.Path)
	c.body = append(c.body, b...)
	c.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (c *capture) snapshot() ([]string, []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.paths...), append([]byte(nil), c.body...)
}

// restoreGlobals undoes the global provider/propagator set by Init.
func restoreGlobals(t *testing.T) {
	t.Helper()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
}

func exportOneSpan(t *testing.T, defaultName string, env map[string]string) (paths []string, body []byte) {
	t.Helper()
	restoreGlobals(t)

	c := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(c.handler))
	t.Cleanup(srv.Close)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_SERVICE_NAME", "")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
	for k, v := range env {
		t.Setenv(k, v)
	}

	shutdown, err := Init(context.Background(), defaultName)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, span := otel.Tracer("test").Start(context.Background(), "unit-span")
	span.End()
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	return c.snapshot()
}

func TestInit_ExportsSpansOverOTLPHTTP(t *testing.T) {
	paths, body := exportOneSpan(t, "fallback-name", nil)

	if len(paths) == 0 || paths[0] != "/v1/traces" {
		t.Fatalf("paths = %v, want a POST to /v1/traces", paths)
	}
	if !bytes.Contains(body, []byte("unit-span")) {
		t.Error("exported payload does not contain the span")
	}
}

func TestInit_UsesDefaultServiceNameWhenEnvUnset(t *testing.T) {
	_, body := exportOneSpan(t, "fallback-name", nil)

	if !bytes.Contains(body, []byte("fallback-name")) {
		t.Error("resource should carry the default service.name")
	}
}

func TestInit_EnvOverridesServiceName(t *testing.T) {
	_, body := exportOneSpan(t, "fallback-name", map[string]string{"OTEL_SERVICE_NAME": "from-env"})

	if !bytes.Contains(body, []byte("from-env")) {
		t.Error("OTEL_SERVICE_NAME should win")
	}
	if bytes.Contains(body, []byte("fallback-name")) {
		t.Error("the default name must not leak when the env var is set")
	}
}

func TestInit_ReadsResourceAttributesFromEnv(t *testing.T) {
	_, body := exportOneSpan(t, "svc", map[string]string{"OTEL_RESOURCE_ATTRIBUTES": "deployment.environment=staging-xyz"})

	if !bytes.Contains(body, []byte("staging-xyz")) {
		t.Error("OTEL_RESOURCE_ATTRIBUTES should reach the exported resource")
	}
}

func TestInit_RegistersW3CPropagators(t *testing.T) {
	exportOneSpan(t, "svc", nil)

	fields := map[string]bool{}
	for _, f := range otel.GetTextMapPropagator().Fields() {
		fields[f] = true
	}
	if !fields["traceparent"] || !fields["baggage"] {
		t.Errorf("propagator fields = %v, want traceparent and baggage", fields)
	}
}

func TestInit_SetsGlobalProviderThatSamplesRoots(t *testing.T) {
	restoreGlobals(t)
	srv := httptest.NewServer(http.HandlerFunc((&capture{}).handler))
	defer srv.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", srv.URL)
	t.Setenv("OTEL_TRACES_SAMPLER", "")

	shutdown, err := Init(context.Background(), "svc")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	_, span := otel.Tracer("t").Start(context.Background(), "root")
	defer span.End()
	if !span.SpanContext().IsSampled() {
		t.Error("root spans must be sampled by default")
	}
}

func TestInit_PropagatorRoundTrip(t *testing.T) {
	exportOneSpan(t, "svc", nil)

	const tp = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	h := propagation.MapCarrier{"traceparent": tp}
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), h)

	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || sc.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("extracted span context = %v, want the incoming trace id", sc)
	}
}
