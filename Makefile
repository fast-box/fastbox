# This Makefile is meant to be used by people that do not usually work
# with Go source code. If you know what GOPATH is then you probably
# don't need to bother with make.

.PHONY: shx android ios shx-cross swarm evm all test clean
.PHONY: shx-linux shx-linux-386 shx-linux-amd64 shx-linux-mips64 shx-linux-mips64le
.PHONY: shx-linux-arm shx-linux-arm-5 shx-linux-arm-6 shx-linux-arm-7 shx-linux-arm64
.PHONY: shx-darwin shx-darwin-386 shx-darwin-amd64
.PHONY: shx-windows shx-windows-386 shx-windows-amd64
.PHONY: docker

GOBIN = $(shell pwd)/build/bin
GOSHX = $(shell pwd)
GO ?= latest

shx:
	build/env.sh go run build/ci.go install ./cmd/shx
	@echo "Done building."
	@echo "Run \"$(GOBIN)/shx\" to launch shx."

promfile:
	build/env.sh go run build/ci.go install ./consensus/promfile
	@echo "Done building."
	@echo "Run \"$(GOBIN)/promfile\" to launch promfile."

all:
	build/env.sh go run build/ci.go install ./cmd/shx
	@echo "Done building."
	@echo "Run \"$(GOBIN)/shx\" to launch shx."
	
	build/env.sh go run build/ci.go install ./consensus/promfile
	@echo "Done building."
	@echo "Run \"$(GOBIN)/promfile\" to launch promfile."

android:
	build/env.sh go run build/ci.go aar --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/shx.aar\" to use the library."

ios:
	build/env.sh go run build/ci.go xcode --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/shx.framework\" to use the library."

test: all
	build/env.sh go run build/ci.go test

clean:
	rm -fr build/workspace/pkg/ $(GOBIN)/*

# The devtools target installs tools required for 'go generate'.
# You need to put $GOBIN (or $GOPATH/bin) in your PATH to use 'go generate'.

devtools:
	env GOBIN= go get -u golang.org/x/tools/cmd/stringer
	env GOBIN= go get -u github.com/jteeuwen/go-bindata/go-bindata
	env GOBIN= go get -u github.com/fjl/gencodec
	env GOBIN= go install ./cmd/abigen

# Cross Compilation Targets (xgo)

shx-cross: shx-linux shx-darwin shx-windows shx-android shx-ios
	@echo "Full cross compilation done:"
	@ls -ld $(GOBIN)/shx-*

shx-linux: shx-linux-386 shx-linux-amd64 shx-linux-arm shx-linux-mips64 shx-linux-mips64le
	@echo "Linux cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-*

shx-linux-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/386 -v ./cmd/shx
	@echo "Linux 386 cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep 386

shx-linux-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/amd64 -v ./cmd/shx
	@echo "Linux amd64 cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep amd64

shx-linux-arm: shx-linux-arm-5 shx-linux-arm-6 shx-linux-arm-7 shx-linux-arm64
	@echo "Linux ARM cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep arm

shx-linux-arm-5:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-5 -v ./cmd/shx
	@echo "Linux ARMv5 cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep arm-5

shx-linux-arm-6:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-6 -v ./cmd/shx
	@echo "Linux ARMv6 cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep arm-6

shx-linux-arm-7:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-7 -v ./cmd/shx
	@echo "Linux ARMv7 cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep arm-7

shx-linux-arm64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm64 -v ./cmd/shx
	@echo "Linux ARM64 cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep arm64

shx-linux-mips:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips --ldflags '-extldflags "-static"' -v ./cmd/shx
	@echo "Linux MIPS cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep mips

shx-linux-mipsle:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mipsle --ldflags '-extldflags "-static"' -v ./cmd/shx
	@echo "Linux MIPSle cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep mipsle

shx-linux-mips64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64 --ldflags '-extldflags "-static"' -v ./cmd/shx
	@echo "Linux MIPS64 cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep mips64

shx-linux-mips64le:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64le --ldflags '-extldflags "-static"' -v ./cmd/shx
	@echo "Linux MIPS64le cross compilation done:"
	@ls -ld $(GOBIN)/shx-linux-* | grep mips64le

shx-darwin: shx-darwin-386 shx-darwin-amd64
	@echo "Darwin cross compilation done:"
	@ls -ld $(GOBIN)/shx-darwin-*

shx-darwin-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/386 -v ./cmd/shx
	@echo "Darwin 386 cross compilation done:"
	@ls -ld $(GOBIN)/shx-darwin-* | grep 386

shx-darwin-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/amd64 -v ./cmd/shx
	@echo "Darwin amd64 cross compilation done:"
	@ls -ld $(GOBIN)/shx-darwin-* | grep amd64

shx-windows: shx-windows-386 shx-windows-amd64
	@echo "Windows cross compilation done:"
	@ls -ld $(GOBIN)/shx-windows-*

shx-windows-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/386 -v ./cmd/shx
	@echo "Windows 386 cross compilation done:"
	@ls -ld $(GOBIN)/shx-windows-* | grep 386

shx-windows-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/amd64 -v ./cmd/shx
	@echo "Windows amd64 cross compilation done:"
	@ls -ld $(GOBIN)/shx-windows-* | grep amd64

docker:
	docker build -t sphinx/shx:latest .
