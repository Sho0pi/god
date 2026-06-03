.PHONY: build test race vet fmt fmt-check lint tidy doctor run-cli run-whatsapp clean

build:
	go build ./...

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "unformatted files:"; gofmt -l .; exit 1)

lint:
	golangci-lint run

tidy:
	go mod tidy

doctor: build
	go run . doctor

run-cli:
	go run . cli

run-whatsapp:
	go run . whatsapp

clean:
	rm -f god
	go clean ./...
