GO ?= go

.PHONY: build test check
build:
	cd linux && mkdir -p bin && $(GO) build -mod=readonly -o bin/server ./cmd/server

test:
	cd linux && dbus-run-session -- $(GO) test -mod=readonly -p 1 -race -count=1 ./...

check:
	cd linux && $(GO) vet -mod=readonly ./...
