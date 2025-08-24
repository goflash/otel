package otel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/goflash/flash/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestOTelMiddlewareDoesNotBlock(t *testing.T) {
	a := flash.New()
	a.Use(OTel("test-svc"))
	a.GET("/", func(c flash.Ctx) error { return c.String(http.StatusOK, "ok") })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestOTelErrorBranch(t *testing.T) {
	a := flash.New()
	a.Use(OTel("svc"))
	a.GET("/u/:id", func(c flash.Ctx) error { return errors.New("boom") })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/u/1", nil)
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 from default error handler, got %d", rec.Code)
	}
}

func TestOTelWithConfig_Options(t *testing.T) {
	a := flash.New()
	a.Use(OTelWithConfig(OTelConfig{
		ServiceName:    "svc",
		RecordDuration: true,
		FilterFunc: func(c flash.Ctx) bool {
			return c.Path() == "/healthz" // skip tracing but proceed
		},
		StatusFunc: func(code int, err error) (codes.Code, string) {
			if code >= 400 && code < 500 {
				return codes.Error, "client error"
			}
			if err != nil || code >= 500 {
				return codes.Error, http.StatusText(code)
			}
			return codes.Ok, ""
		},
	}))

	a.GET("/", func(c flash.Ctx) error { return c.String(http.StatusOK, "ok") })
	a.GET("/healthz", func(c flash.Ctx) error { return c.String(http.StatusOK, "ok") })
	a.GET("/bad", func(c flash.Ctx) error { return c.String(http.StatusBadRequest, "bad") })

	for path, want := range map[string]int{"/": http.StatusOK, "/healthz": http.StatusOK, "/bad": http.StatusBadRequest} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		a.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s: got %d want %d", path, rec.Code, want)
		}
	}
}

func TestOTelWithConfig_CustomizationsBranches(t *testing.T) {
	// Use no-op tracer and a no-op propagator to exercise non-nil paths
	noopTracer := trace.NewNoopTracerProvider().Tracer("test")
	noopProp := propagation.NewCompositeTextMapPropagator()

	a := flash.New()
	a.Use(OTelWithConfig(OTelConfig{
		Tracer:      noopTracer,
		Propagator:  noopProp,
		ServiceName: "svc2",
		SpanNameFunc: func(c flash.Ctx) string {
			// Return empty to ensure default branch fallback
			return ""
		},
		AttributesFunc: func(c flash.Ctx) []attribute.KeyValue {
			return []attribute.KeyValue{attribute.String("custom.attr", "v")}
		},
		ExtraAttributes: []attribute.KeyValue{attribute.String("extra.attr", "x")},
		StatusFunc: func(code int, err error) (codes.Code, string) {
			// Explicitly mark OK with custom description
			return codes.Ok, ""
		},
	}))

	a.GET("/x", func(c flash.Ctx) error {
		// set route name to ensure http.route attribute path covered
		return c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(context.Background())
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
}

func TestOTelWithConfig_SpanNameOverride_And_NoWrite(t *testing.T) {
	a := flash.New()
	a.Use(OTelWithConfig(OTelConfig{
		ServiceName:  "svc3",
		SpanNameFunc: func(c flash.Ctx) string { return "CUSTOM NAME" }, // non-empty override branch
		// default Status mapping used; ensure default branch is exercised
	}))

	// Handler writes nothing and returns nil -> status remains 0 inside middleware, should default to 200
	a.GET("/empty", func(c flash.Ctx) error { return nil })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/empty", nil)
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected default 200 when no write, got %d", rec.Code)
	}
}

func TestHelpers_NoActiveSpan_NoPanicAndNotRecording(t *testing.T) {
	a := flash.New()

	var isRecording bool
	a.GET("/nop", func(c flash.Ctx) error {
		sp := Span(c)
		isRecording = sp.IsRecording()
		// Should be safe no-ops
		AddAttributes(c, attribute.String("k", "v"))
		AddEvent(c, "evt")
		return c.String(http.StatusNoContent, "")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nop", nil)
	a.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("code=%d", rec.Code)
	}
	if isRecording {
		t.Fatalf("expected non-recording span when no middleware active")
	}
}

func TestHelpers_WithActiveSpan_AttributesAndEventsCaptured(t *testing.T) {
	// Setup tracer provider with a span recorder
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	a := flash.New()
	a.Use(OTel("svc"))

	a.GET("/dyn/:id", func(c flash.Ctx) error {
		if !Span(c).IsRecording() {
			t.Fatalf("expected recording span")
		}
		AddAttributes(c, attribute.String("dyn.k", "v"), attribute.Int("dyn.n", 7))
		AddEvent(c, "custom.event", attribute.String("ev.k", "evv"))
		return c.String(http.StatusOK, "ok")
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dyn/1", nil)
	a.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatalf("expected at least one span, got 0")
	}
	// Find server span for our request
	var got sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() != "GET /dyn/:id" && s.Name() != "GET /dyn/1" {
			// keep looking; name could be route or path depending on when attributes/stage ran
			continue
		}
		got = s
		break
	}
	if got == nil {
		// fallback to first span
		got = spans[0]
	}

	// Verify dynamic attributes present
	attrs := got.Attributes()
	foundK, foundN := false, false
	for _, kv := range attrs {
		if kv.Key == "dyn.k" && kv.Value.AsString() == "v" {
			foundK = true
		}
		if kv.Key == "dyn.n" && kv.Value.AsInt64() == 7 {
			foundN = true
		}
	}
	if !foundK || !foundN {
		t.Fatalf("dynamic attributes missing: k=%v n=%v attrs=%v", foundK, foundN, attrs)
	}

	// Verify custom event is attached
	evs := got.Events()
	foundEvent := false
	for _, ev := range evs {
		if ev.Name == "custom.event" {
			foundEvent = true
			// Optional: check event attribute
			hasAttr := false
			for _, eattr := range ev.Attributes {
				if eattr.Key == "ev.k" && eattr.Value.AsString() == "evv" {
					hasAttr = true
					break
				}
			}
			if !hasAttr {
				t.Fatalf("custom.event missing attribute ev.k=evv: %#v", ev.Attributes)
			}
			break
		}
	}
	if !foundEvent {
		t.Fatalf("custom.event not found on span; events=%v", evs)
	}
}
