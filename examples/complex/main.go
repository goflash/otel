package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/goflash/flash/v2"
	"github.com/goflash/otel/v2"
	otelglobal "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func setupTracer(service string) (func(context.Context) error, error) {
	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", service),
		attribute.String("env", "dev"),
	))
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otelglobal.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

func main() {
	shutdown, err := setupTracer("example-complex")
	if err != nil {
		log.Fatalf("setup tracer: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	app := flash.New()

	app.Use(otel.OTelWithConfig(otel.OTelConfig{
		ServiceName:    "example-complex",
		RecordDuration: true,
		// Skip health checks
		FilterFunc: func(c flash.Ctx) bool { return c.Path() == "/healthz" },
		// Customize span name
		SpanNameFunc: func(c flash.Ctx) string {
			if r := c.Route(); r != "" {
				return c.Method() + " " + r
			}
			return c.Method() + " " + c.Path()
		},
		// Add computed attributes at the end of request
		AttributesFunc: func(c flash.Ctx) []attribute.KeyValue {
			ua := c.Request().UserAgent()
			return []attribute.KeyValue{
				attribute.String("http.user_agent", ua),
			}
		},
		// Custom status mapping
		StatusFunc: func(code int, err error) (codes.Code, string) {
			if code >= 400 && code < 500 {
				return codes.Error, "client error"
			}
			if err != nil || code >= 500 {
				return codes.Error, http.StatusText(code)
			}
			return codes.Ok, ""
		},
		ExtraAttributes: []attribute.KeyValue{
			attribute.String("app.region", "local"),
		},
	}))

	app.GET("/healthz", func(c flash.Ctx) error { return c.String(http.StatusNoContent, "") })
	app.GET("/", func(c flash.Ctx) error { return c.String(http.StatusOK, "complex ok") })

	app.GET("/slow", func(c flash.Ctx) error {
		time.Sleep(120 * time.Millisecond)
		return c.String(http.StatusOK, "slow done")
	})

	app.GET("/attrs", func(c flash.Ctx) error {
		// Demonstrate dynamic attributes and events
		otel.AddAttributes(c, attribute.String("user.id", "u-42"))
		otel.AddEvent(c, "custom.event", attribute.String("note", "from handler"))
		return c.String(http.StatusOK, "attrs set")
	})

	app.GET("/err", func(c flash.Ctx) error {
		// Return an error to see span status and error recording
		return errors.New("boom")
	})

	log.Fatal(http.ListenAndServe(":8080", app))
}
