.PHONY: proto build test clean

proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       pkg/proto/*.proto

build:
	go build -o bin/cache-node cmd/cache-node/main.go
	go build -o bin/cache-client cmd/cache-client/main.go

test:
	go test -v -race ./...

clean:
	rm -rf bin/
	find . -name "*.pb.go" -delete
