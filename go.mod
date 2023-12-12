module github.com/GoogleCloudPlatform/guest-agent

go 1.20

replace github.com/GoogleCloudPlatform/guest-agent/metadata => ../metadata

require (
	cloud.google.com/go/storage v1.31.0
	github.com/GoogleCloudPlatform/guest-logging-go v0.0.0-20230710215706-450679fd88a9
	github.com/go-ini/ini v1.66.6
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da
	github.com/golang/protobuf v1.5.3
	github.com/google/go-cmp v0.5.9
	github.com/google/go-tpm v0.9.0
	github.com/google/go-tpm-tools v0.4.0
	github.com/google/tink/go v1.7.0
	github.com/kardianos/service v1.2.1
	github.com/robfig/cron/v3 v3.0.1
	github.com/tarm/serial v0.0.0-20180830185346-98f6abe2eb07
	golang.org/x/crypto v0.11.0
	golang.org/x/sys v0.11.0
	google.golang.org/grpc v1.57.0
	google.golang.org/protobuf v1.31.0
	software.sslmate.com/src/go-pkcs12 v0.2.1
)

require (
	cloud.google.com/go v0.110.6 // indirect
	cloud.google.com/go/compute v1.23.0 // indirect
	cloud.google.com/go/compute/metadata v0.2.3 // indirect
	cloud.google.com/go/iam v1.1.1 // indirect
	cloud.google.com/go/logging v1.7.0 // indirect
	cloud.google.com/go/longrunning v0.5.1 // indirect
	github.com/Microsoft/go-winio v0.6.1 // indirect
	github.com/google/go-sev-guest v0.7.0 // indirect
	github.com/google/logger v1.1.1 // indirect
	github.com/google/s2a-go v0.1.4 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.2.5 // indirect
	github.com/googleapis/gax-go/v2 v2.12.0 // indirect
	github.com/pborman/uuid v1.2.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	go.opencensus.io v0.24.0 // indirect
	golang.org/x/mod v0.8.0 // indirect
	golang.org/x/net v0.12.0 // indirect
	golang.org/x/oauth2 v0.10.0 // indirect
	golang.org/x/sync v0.3.0 // indirect
	golang.org/x/text v0.11.0 // indirect
	golang.org/x/tools v0.6.0 // indirect
	golang.org/x/xerrors v0.0.0-20220907171357-04be3eba64a2 // indirect
	google.golang.org/api v0.134.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20230726155614-23370e0ffb3e // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20230726155614-23370e0ffb3e // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230726155614-23370e0ffb3e // indirect
)
