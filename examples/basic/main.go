package main

import (
	"context"
	"log"
	"net/http"

	"github.com/goflash/flash/v2"
	"github.com/goflash/otel/v2"
	otelglobal "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// setupTracer configures a simple stdout exporter and tracer provider.
func setupTracer(service string) (func(context.Context) error, error) {
	exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", service),
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
	shutdown, err := setupTracer("example-basic")
	if err != nil {
		log.Fatalf("setup tracer: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	app := flash.New()

	// Minimal usage: just set a service name.
	app.Use(otel.OTel("example-basic"))

	app.GET("/", func(c flash.Ctx) error { return c.String(http.StatusOK, "ok") })
	app.GET("/hello/:name", func(c flash.Ctx) error {
		// Add dynamic attributes/events to the active span
		otel.AddAttributes(c, attribute.String("request.name", c.Param("name")))
		otel.AddEvent(c, "handler.hello")
		return c.String(http.StatusOK, "hello "+c.Param("name"))
	})

	log.Fatal(http.ListenAndServe(":8080", app))
}
