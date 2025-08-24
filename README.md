# OpenTelemetry middleware for the GoFlash web framework

<h1 align="center">
    <a href="https://pkg.go.dev/github.com/goflash/otel/v2@v2.0.1">
        <img src="https://pkg.go.dev/badge/github.com/goflash/otel.svg" alt="Go Reference">
    </a>
    <a href="https://goreportcard.com/report/github.com/goflash/otel">
        <img src="https://img.shields.io/badge/%F0%9F%93%9D%20Go%20Report-A%2B-75C46B?style=flat-square" alt="Go Report Card">
    </a>
    <a href="https://codecov.io/gh/goflash/otel">
        <img src="https://codecov.io/gh/goflash/otel/graph/badge.svg?token=VRHM48HJ5L" alt="Coverage">
    </a>
    <a href="https://github.com/goflash/otel/actions?query=workflow%3ATest">
        <img src="https://img.shields.io/github/actions/workflow/status/goflash/otel/test-coverage.yml?branch=main&label=%F0%9F%A7%AA%20Tests&style=flat-square&color=75C46B" alt="Tests">
    </a>
    <img src="https://img.shields.io/badge/go-1.23%2B-00ADD8?logo=golang" alt="Go Version">
    <a href="https://docs.goflash.dev">
        <img src="https://img.shields.io/badge/%F0%9F%92%A1%20GoFlash-docs-00ACD7.svg?style=flat-square" alt="GoFlash Docs">
    </a>
    <img src="https://img.shields.io/badge/status-stable-green" alt="Status">
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="License">
    <br>
    <div style="text-align:center">
      <a href="https://discord.gg/QHhGHtjjQG">
        <img src="https://dcbadge.limes.pink/api/server/https://discord.gg/QHhGHtjjQG" alt="Discord">
      </a>
    </div>
</h1>

Lightweight, configurable OpenTelemetry tracing middleware for the GoFlash framework. It creates a server span for each incoming request, sets useful HTTP attributes, propagates context, records errors, and plays nicely with your existing TracerProvider and propagators.

## Features

- Server span per request (SpanKindServer)
- Smart span names: METHOD ROUTE (falls back to METHOD PATH)
- Context propagation using your configured TextMapPropagator
- HTTP attributes: method, target, route, status_code, peer addr, and optional duration
- Customizable filtering, naming, attributes, and status mapping
- Simple helper OTel(serviceName, extraAttrs...) or full OTelWithConfig(cfg)
- Per-request helpers to add dynamic span attributes/events from handlers

## Installation

```sh
go get github.com/goflash/otel/v2
```

Go version: requires Go 1.23+. The module sets `go 1.23` and can be used with newer Go versions. If you use `GOTOOLCHAIN=auto`, the `toolchain` directive will ensure a compatible toolchain is used.

## Quick start

```go
import (
    "github.com/goflash/flash/v2"
    "github.com/goflash/otel/v2"
    "go.opentelemetry.io/otel/attribute"
)

func main() {
    a := flash.New()

    // Minimal: just set a service name.
    a.Use(otel.OTel("users-api"))

    a.GET("/hello/:name", func(c flash.Ctx) error {

    // Dynamically attach attributes to the active span
    otel.AddAttributes(c, attribute.String("request.name", c.Param("name")))
    otel.AddEvent(c, "handler.started")
        return c.String(200, "hello "+c.Param("name"))
    })

    _ = a.Start(":8080")
}
```

## Configuration

Use `OTelWithConfig` for full control:

```go
mw := otel.OTelWithConfig(otel.OTelConfig{
    // Tracer: optional. Defaults to otel.Tracer("github.com/goflash/flash/v2").
    // Propagator: optional. Defaults to otel.GetTextMapPropagator().

    // Skip certain requests (e.g., health checks):
    FilterFunc: func(c flash.Ctx) bool { return c.Path() == "/healthz" },

    // Customize span name (default: "METHOD ROUTE" or "METHOD PATH"):
    SpanNameFunc: func(c flash.Ctx) string { return "API " + c.Method() + " " + c.Route() },

    // Add attributes computed at request end:
    AttributesFunc: func(c flash.Ctx) []attribute.KeyValue {
        return []attribute.KeyValue{attribute.String("app.user_id", c.Get("uid"))}
    },

    // Control span status mapping (default: Error for err!=nil or status>=500):
    StatusFunc: func(code int, err error) (codes.Code, string) {
        if code >= 400 && code < 500 {
            return codes.Error, "client error"
        }
        if err != nil || code >= 500 {
            return codes.Error, http.StatusText(code)
        }
        return codes.Ok, ""
    },

    // Also record request duration in ms (float64) under http.server.duration_ms:
    RecordDuration: true,

    // Convenience: set service.name if you don't set it on the Resource:
    ServiceName: "users-api",

    // Extra static attributes:
    ExtraAttributes: []attribute.KeyValue{attribute.String("app.region", "us-east-1")},
})

a.Use(mw)
```

### Dynamic attributes/events from handlers

In addition to `AttributesFunc` configured on the middleware, you can attach attributes and events from inside any route handler:

```go
import (
    "github.com/goflash/flash/v2"
    "github.com/goflash/otel/v2"
    "go.opentelemetry.io/otel/attribute"
)

a.GET("/orders/:id", func(c flash.Ctx) error {
    id := c.Param("id")
    otel.AddAttributes(c,
        attribute.String("order.id", id),
        attribute.String("user.id", c.Get("uid")),
    )
    otel.AddEvent(c, "db.lookup.start")
    // ... do work ...
    otel.AddEvent(c, "db.lookup.done")
    return c.NoContent(204)
})
```

### Attributes set

The middleware sets these attributes by default:

- http.method
- http.target (raw path)
- http.route (if available after routing)
- http.status_code
- net.peer.addr
- service.name (if ServiceName provided; prefer setting via Resource)
- http.server.duration_ms (if RecordDuration enabled)

Plus anything returned by `Attributes`, `ExtraAttributes`, and on-demand via `AddAttributes`.

### Context propagation

Incoming context is extracted from headers using the configured propagator (defaults to `otel.GetTextMapPropagator()`). A server span is started and the updated context is put back onto the request, so downstream outbound calls can use the same context for trace continuity.

### Errors and status

- If the handler returns an error, it is recorded on the span and the span status is set via `Status` mapping.
- When no status is written by the handler, the middleware treats it as 200 OK for status mapping.

## End-to-end example with SDK

```go
import (
    "context"
    "log"

    "github.com/goflash/flash/v2"
    "github.com/goflash/otel/v2"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    "go.opentelemetry.io/otel/propagation"
)

func main() {
    ctx := context.Background()

    // Example exporter; configure as needed.
    exp, _ := otlptracehttp.New(ctx)
    tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp))
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})

    a := flash.New()
    a.Use(otel.OTel("users-api"))
    a.GET("/", func(c flash.Ctx) error { return c.String(200, "ok") })

    if err := a.Start(":8080"); err != nil {
        log.Fatal(err)
    }
}
```

## Examples

Two runnable examples are included:

- examples/basic: minimal setup with stdout exporter and the default middleware.
- examples/complex: full-featured configuration (custom naming, filtering, status mapping, extra attributes, and duration).

Try them locally (they use a replace to this module):

```sh
cd examples/basic && go run .
# in another terminal
curl -s localhost:8080/

cd ../../examples/complex && go run .
```

## Versioning and compatibility

- Module path: `github.com/goflash/otel/v2`
- Requires Go 1.23+
- Works with any OpenTelemetry SDK/propagator you configure in your app

## Contributing

Issues and PRs are welcome. Please run tests before submitting:

```sh
go test ./...
```

## License

MIT
