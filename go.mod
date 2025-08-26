module github.com/goflash/otel/v2

go 1.23.0

toolchain go1.23.2

require (
	github.com/goflash/flash/v2 v2.0.0-beta.8
	go.opentelemetry.io/otel v1.37.0
	go.opentelemetry.io/otel/sdk v1.37.0
	go.opentelemetry.io/otel/trace v1.37.0
)

require (
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/julienschmidt/httprouter v1.3.0 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel/metric v1.37.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
)

// replace github.com/goflash/flash/v2 => ../flash
