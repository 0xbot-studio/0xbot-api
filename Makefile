run:
	go run ./cmd/server
build:
	go build -o bin/0xbot-api ./cmd/server
fmt:
	gofmt -w cmd internal
lint:
	go vet ./...
test:
	go test ./...
race:
	go test -race ./...

