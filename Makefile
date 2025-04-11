.PHONY: test

build:
	go build -ldflags "-X main.Version=$(DOCKER_IMAGE_VERSION)" -o bin/api ./cmd/api/

test:
	go test $$(go list ./... | grep -v /vendor/) -race -coverprofile cover.out -timeout 120s
