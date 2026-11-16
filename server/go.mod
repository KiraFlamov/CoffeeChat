module git.sourcecraft.dev/drflamov/coffeechat/server

go 1.27

require (
	connectrpc.com/connect v1.21.1
	github.com/coder/websocket v1.8.15
	github.com/ydb-platform/ydb-go-sdk/v3 v3.157.0
	github.com/ydb-platform/ydb-go-yc-metadata v0.6.2
	golang.org/x/crypto/x509roots/fallback v0.0.0-20261113174652-9ccaac6145fb
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	github.com/ydb-platform/ydb-go-genproto v0.0.0-20260810123728-f0c151ab31b9 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261110000943-09e21b237f2c // indirect
	google.golang.org/grpc v1.85.0 // indirect
)

tool (
	connectrpc.com/connect/cmd/protoc-gen-connect-go
	google.golang.org/protobuf/cmd/protoc-gen-go
)
