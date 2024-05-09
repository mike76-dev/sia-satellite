# These variables get inserted into build/commit.go
GIT_REVISION=$(shell git rev-parse --short HEAD)
GIT_DIRTY=$(shell git diff-index --quiet HEAD -- || echo "✗-")
ifeq ("$(GIT_DIRTY)", "✗-")
	BUILD_TIME=$(shell date)
else
	BUILD_TIME=$(shell git show -s --format=%ci HEAD)
endif

ldflags= \
-X "github.com/mike76-dev/sia-satellite/internal/build.BinaryName=satd" \
-X "github.com/mike76-dev/sia-satellite/internal/build.NodeVersion=0.11.4" \
-X "github.com/mike76-dev/sia-satellite/internal/build.GitRevision=${GIT_DIRTY}${GIT_REVISION}" \
-X "github.com/mike76-dev/sia-satellite/internal/build.BuildTime=${BUILD_TIME}"

# all will build and install release binaries
all: release

# pkgs changes which packages the makefile calls operate on.
pkgs = \
	./external \
	./mail \
	./modules \
	./modules/consensus \
	./modules/gateway \
	./modules/manager \
	./modules/manager/contractor \
	./modules/manager/contractor/contractset \
	./modules/manager/hostdb \
	./modules/manager/hostdb/hosttree \
	./modules/manager/proto \
	./modules/portal \
	./modules/provider \
	./modules/transactionpool \
	./modules/wallet \
	./node \
	./node/api \
	./node/api/client \
	./node/api/server \
	./persist \
	./satc \
	./satd

# release-pkgs determine which packages are built for release and distribution
# when running a 'make release' command.
release-pkgs = ./satc ./satd

# lockcheckpkgs are the packages that are checked for locking violations.
lockcheckpkgs = \
	./node \
	./node/api \
	./node/api/client \
	./node/api/server \
	./satc \
	./satd \

# dependencies list all packages needed to run make commands used to build
# and lint satc/satd locally and in CI systems.
dependencies:
	go get -d ./...

# fmt calls go fmt on all packages.
fmt:
	gofmt -s -l -w $(pkgs)

# vet calls go vet on all packages.
# NOTE: go vet requires packages to be built in order to obtain type info.
vet:
	go vet $(pkgs)

static:
	go build -trimpath -o release/ -tags='netgo' -ldflags='-s -w $(ldflags)' $(release-pkgs)

# release builds and installs release binaries.
release:
	go install -tags='netgo' -ldflags='-s -w $(ldflags)' $(release-pkgs)

# clean removes all directories that get automatically created during
# development.
clean:
ifneq ("$(OS)","Windows_NT")
# Linux
	rm -rf release
else
# Windows
	- DEL /F /Q release
endif

.PHONY: all fmt install release clean

