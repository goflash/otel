// Package middleware provides HTTP middleware for flash.
package otel

import (
	"net/http"
	"time"

	"github.com/goflash/flash/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// OTelConfig configures the OpenTelemetry middleware.
// All fields are optional; sensible defaults are used when not provided.
type OTelConfig struct {
	// Tracer to use. If nil, otel.Tracer("github.com/goflash/flash/v2") is used.
	Tracer trace.Tracer
	// Propagator to extract context. If nil, otel.GetTextMapPropagator() is used.
	Propagator propagation.TextMapPropagator
	// FilterFunc returns true to skip tracing for a request (e.g., health checks).
	FilterFunc func(flash.Ctx) bool
	// SpanNameFunc formats the span name. If nil, defaults to "METHOD ROUTE" or "METHOD PATH".
	SpanNameFunc func(flash.Ctx) string
	// AttributesFunc returns additional attributes to set on the span at the end of the request.
	AttributesFunc func(flash.Ctx) []attribute.KeyValue
	// StatusFunc maps HTTP status and error to span status code/description. If nil, defaults to
	// Error for err!=nil or status>=500, else Ok.
	StatusFunc func(status int, err error) (codes.Code, string)
	// RecordDuration enables recording request duration in milliseconds as attribute
	// "http.server.duration_ms" (float64).
	RecordDuration bool
	// ServiceName, if set, adds attribute service.name. Prefer setting service name on Resource
	// in your TracerProvider; this is offered as a convenience.
	ServiceName string
	// ExtraAttributes are appended to span attributes.
	ExtraAttributes []attribute.KeyValue
}

// OTel returns middleware that creates an OpenTelemetry server span for each request.
// Kept for convenience; delegates to OTelWithConfig with service name and extra attributes.
func OTel(serviceName string, extraAttrs ...attribute.KeyValue) flash.Middleware {
	return OTelWithConfig(OTelConfig{ServiceName: serviceName, ExtraAttributes: extraAttrs})
}

// OTelWithConfig returns middleware that creates an OpenTelemetry server span using cfg.
func OTelWithConfig(cfg OTelConfig) flash.Middleware {
	tracer := cfg.Tracer
	if tracer == nil {
		tracer = otel.Tracer("GoFlash")
	}
	prop := cfg.Propagator
	if prop == nil {
		prop = otel.GetTextMapPropagator()
	}

	defaultSpanName := func(c flash.Ctx) string {
		name := c.Method() + " " + c.Path()
		if rt := c.Route(); rt != "" {
			name = c.Method() + " " + rt
		}
		return name
	}

	defaultStatus := func(status int, err error) (codes.Code, string) {
		if err != nil || status >= 500 {
			return codes.Error, http.StatusText(status)
		}
		return codes.Ok, ""
	}

	return func(next flash.Handler) flash.Handler {
		return func(c flash.Ctx) error {
			// Optionally skip tracing
			if cfg.FilterFunc != nil && cfg.FilterFunc(c) {
				return next(c)
			}

			r := c.Request()

			// Build span name
			name := defaultSpanName(c)
			if cfg.SpanNameFunc != nil {
				if n := cfg.SpanNameFunc(c); n != "" {
					name = n
				}
			}

			// Extract remote context and start a server span
			reqCtx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			reqCtx, span := tracer.Start(reqCtx, name, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			// propagate context into request for downstream calls
			r = r.WithContext(reqCtx)
			c.SetRequest(r)

			start := time.Now()
			err := next(c)
			elapsed := time.Since(start)

			status := c.StatusCode()
			if status == 0 {
				status = http.StatusOK
			}

			// Base attributes (computed late to include route if set post-match)
			attrs := []attribute.KeyValue{
				attribute.String("http.method", c.Method()),
				attribute.String("http.target", c.Path()),
				attribute.String("net.peer.addr", r.RemoteAddr),
			}
			if rt := c.Route(); rt != "" {
				attrs = append(attrs, attribute.String("http.route", rt))
			}
			if cfg.ServiceName != "" {
				attrs = append(attrs, attribute.String("service.name", cfg.ServiceName))
			}
			if cfg.AttributesFunc != nil {
				attrs = append(attrs, cfg.AttributesFunc(c)...)
			}
			if len(cfg.ExtraAttributes) > 0 {
				attrs = append(attrs, cfg.ExtraAttributes...)
			}
			attrs = append(attrs, attribute.Int("http.status_code", status))
			if cfg.RecordDuration {
				attrs = append(attrs, attribute.Float64("http.server.duration_ms", float64(elapsed)/float64(time.Millisecond)))
			}

			span.SetAttributes(attrs...)

			code, desc := defaultStatus(status, err)
			if cfg.StatusFunc != nil {
				code, desc = cfg.StatusFunc(status, err)
			}
			span.SetStatus(code, desc)
			if err != nil {
				span.RecordError(err)
			}

			return err
		}
	}
}

// Span returns the active tracing span from the request context stored in c.
// It is safe to call even if tracing is disabled; a no-op span will be returned.
func Span(c flash.Ctx) trace.Span {
	return trace.SpanFromContext(c.Request().Context())
}

// AddAttributes sets attributes on the active span for this request.
// If no span is active, this is a no-op.
func AddAttributes(c flash.Ctx, attrs ...attribute.KeyValue) {
	Span(c).SetAttributes(attrs...)
}

// AddEvent adds an event with optional attributes to the active span.
// If no span is active, this is a no-op.
func AddEvent(c flash.Ctx, name string, attrs ...attribute.KeyValue) {
	Span(c).AddEvent(name, trace.WithAttributes(attrs...))
}
