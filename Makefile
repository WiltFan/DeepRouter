VERSION ?= 0.1.0
PREFIX ?= $(HOME)/.local/bin
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: test build install dist

test:
	go test ./...

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-setup .

install:
	sh install.sh --prefix "$(PREFIX)" --from-source

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-setup-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-setup-linux-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-setup-darwin-amd64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-setup-darwin-arm64 .
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/deeprouter-setup-windows-amd64.exe .
	cd dist && sha256sum deeprouter-setup-* > checksums.txt
