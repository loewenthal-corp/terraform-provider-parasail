.PHONY: build test fmt tidy

build:
	go build -o bin/terraform-provider-parasail ./cmd/terraform-provider-parasail

test:
	go test ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.terraform/*')

tidy:
	go mod tidy

