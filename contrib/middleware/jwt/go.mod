module github.com/go-kratos/kratos/contrib/middleware/jwt/v3

go 1.25.0

require (
	github.com/go-kratos/kratos/v3 v3.0.0
	github.com/golang-jwt/jwt/v5 v5.3.1
)

require (
	github.com/go-playground/form/v4 v4.3.0 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.6 // indirect
	golang.org/x/sys v0.47.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260723215102-3fe39f3c1018 // indirect
	google.golang.org/grpc v1.82.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/go-kratos/kratos/v3 => ../../..
