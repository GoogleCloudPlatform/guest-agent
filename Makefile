ENVVAR=CGO_ENABLED=0
GOOS?=linux
GOARCH?=amd64

.PHONY: all
all: build

.PHONY: build
build: google_guest_agent

.PHONY: google_guest_agent
google_guest_agent:
	./build_google_guest_agent.sh "20230601.00"

.PHONY: clean
clean:
	rm -rf ./out
