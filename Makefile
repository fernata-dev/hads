.PHONY: build test bench dist svg

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$$(git describe --tags --always 2>/dev/null || echo dev)" -o hads ./cmd/hads

test:
	go vet ./...
	go test ./...
	@if [ "$$(go env GOOS)" = linux ]; then go test -race ./...; fi

bench:
	go test -run xxx -bench . -benchmem ./internal/model/

dist:
	./scripts/build.sh

svg:
	python3 docs/gen_svg.py
