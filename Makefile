export GOPRIVATE=""
export GOPROXY=
export GOSUMDB=

ifndef $(GOPATH)
    GOPATH=$(shell go env GOPATH)
    export GOPATH
endif


.PHONY: all deps bin clean

all:
	@$(MAKE) bin

deps:
	go mod tidy
	go mod download

bin:
	go build -o bin/kaf-cli cmd/cli.go

clean:
	rm -rf bin

install: bin
	cp bin/kaf-cli $(GOPATH)/bin
