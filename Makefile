NAME=jenkins-job-cli
VERSION=$(shell cat VERSION)
BUILD=$(shell git rev-parse --short HEAD)
EXT_LD_FLAGS="-Wl,--allow-multiple-definition"
LD_FLAGS="-w -X main.version=$(VERSION) -X main.build=$(BUILD) -extldflags=$(EXT_LD_FLAGS)"


clean:
	rm -rf _build/ release/

install:
	go mod download
	CGO_ENABLED=0 go build -tags release -ldflags $(LD_FLAGS) -o jenkins-job-cli
	mv jenkins-job-cli /opt/homebrew/bin/jj

build:
	go mod download
	CGO_ENABLED=0 go build -tags release -ldflags $(LD_FLAGS) -o jenkins-job-cli

build-dev:
	go build -ldflags "-w -X main.version=$(VERSION)-dev -X main.build=$(BUILD) -extldflags=$(EXT_LD_FLAGS)"

build-all:
	mkdir -p _build
	GOOS=darwin  GOARCH=amd64 go build -tags release -ldflags $(LD_FLAGS) -o _build/jenkins-job-cli-$(VERSION)-darwin-amd64
	GOOS=darwin  GOARCH=arm64 go build -tags release -ldflags $(LD_FLAGS) -o _build/jenkins-job-cli-$(VERSION)-darwin-arm64
	GOOS=linux   GOARCH=amd64 go build -tags release -ldflags $(LD_FLAGS) -o _build/jenkins-job-cli-$(VERSION)-linux-amd64
	GOOS=linux   GOARCH=arm   go build -tags release -ldflags $(LD_FLAGS) -o _build/jenkins-job-cli-$(VERSION)-linux-arm
	GOOS=linux   GOARCH=arm64 go build -tags release -ldflags $(LD_FLAGS) -o _build/jenkins-job-cli-$(VERSION)-linux-arm64
	GOOS=windows GOARCH=amd64 go build -tags release -ldflags $(LD_FLAGS) -o _build/jenkins-job-cli-$(VERSION)-windows-amd64
	cd _build; sha256sum * > sha256sums.txt

image:
	docker build -t jenkins-job-cli -f Dockerfile .

release: build-all
	rm -rf release
	mkdir release
	cp _build/* release
	cd release; sha256sum --quiet --check sha256sums.txt
	git tag v$(VERSION) && git push origin v$(VERSION)
	ghr v$(VERSION) release

.PHONY: build
