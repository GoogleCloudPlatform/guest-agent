module github.com/GoogleCloudPlatform/guest-agent

go 1.25

replace github.com/GoogleCloudPlatform/guest-agent/metadata => ../metadata

require (
	cloud.google.com/go/storage v1.56.1
	github.com/GoogleCloudPlatform/guest-logging-go v0.0.0-20250819203904-a74b641fa7fd
	github.com/Microsoft/go-winio v0.6.2
	github.com/go-ini/ini v1.67.0
	github.com/golang/groupcache v0.0.0-20241129210726-2c02b8208cf8
	github.com/google/go-cmp v0.7.0
	github.com/google/go-tpm v0.9.5
	github.com/google/go-tpm-tools v0.4.5
	github.com/google/tink/go v1.7.0
	github.com/kardianos/service v1.2.4
	github.com/robfig/cron/v3 v3.0.1
	github.com/tarm/serial v0.0.0-20180830185346-98f6abe2eb07
	golang.org/x/crypto v0.41.0
	golang.org/x/sys v0.35.0
	google.golang.org/api v0.248.0
	google.golang.org/grpc v1.75.0
	google.golang.org/protobuf v1.36.8
	gopkg.in/yaml.v3 v3.0.1
	software.sslmate.com/src/go-pkcs12 v0.6.0
)

require (
	cel.dev/expr v0.24.0 // indirect
	cloud.google.com/go v0.121.6 // indirect
	cloud.google.com/go/auth v0.16.5 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.8.0 // indirect
	cloud.google.com/go/iam v1.5.2 // indirect
	cloud.google.com/go/logging v1.13.0 // indirect
	cloud.google.com/go/longrunning v0.6.7 // indirect
	cloud.google.com/go/monitoring v1.24.2 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.29.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric v0.53.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping v0.53.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cncf/xds/go v0.0.0-20250501225837-2ac532fd4443 // indirect
	github.com/envoyproxy/go-control-plane/envoy v1.32.4 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.2.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-jose/go-jose/v4 v4.1.2 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/go-configfs-tsm v0.3.3 // indirect
	github.com/google/go-sev-guest v0.13.0 // indirect
	github.com/google/go-tdx-guest v0.3.2-0.20241009005452-097ee70d0843 // indirect
	github.com/google/logger v1.1.1 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.6 // indirect
	github.com/googleapis/gax-go/v2 v2.15.0 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/spiffe/go-spiffe/v2 v2.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/detectors/gcp v1.37.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.62.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.62.0 // indirect
	go.opentelemetry.io/otel v1.37.0 // indirect
	go.opentelemetry.io/otel/metric v1.37.0 // indirect
	go.opentelemetry.io/otel/sdk v1.37.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.37.0 // indirect
	go.opentelemetry.io/otel/trace v1.37.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/sync v0.16.0 // indirect
	golang.org/x/text v0.28.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	google.golang.org/genproto v0.0.0-20250826171959-ef028d996bc1 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20250826171959-ef028d996bc1 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250826171959-ef028d996bc1 // indirect
)
