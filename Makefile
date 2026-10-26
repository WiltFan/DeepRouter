VERSION ?= 0.1.0
PREFIX ?= $(HOME)/.local/bin
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: test build install dist

test:
	go test ./...

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter .

install:
	sh install.sh --prefix "$(PREFIX)" --from-source

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-linux-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-darwin-amd64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-darwin-arm64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-windows-amd64.exe .
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-windows-arm64.exe .
	cd dist && sha256sum deeprouter-* > checksums.txt
