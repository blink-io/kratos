module github.com/go-kratos/kratos/contrib/opensergo/v3

go 1.25.0

require (
	github.com/go-kratos/kratos/v3 v3.0.0
	github.com/opensergo/opensergo-go v0.0.0-20220331070310-e5b01fee4d1c
	google.golang.org/genproto/googleapis/api v0.0.0-20260723215102-3fe39f3c1018
	google.golang.org/grpc v1.82.1
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/go-playground/form/v4 v4.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260723215102-3fe39f3c1018 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/go-kratos/kratos/v3 => ../../
