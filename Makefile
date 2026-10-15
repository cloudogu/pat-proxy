PACKAGES=$(shell go list ./... | grep -v /vendor/)

ARTIFACT_ID=pat-proxy
VERSION=0.1.0
BUILD_TIME:=$(shell date +%FT%T%z)
COMMIT_ID:=$(shell git rev-parse HEAD)
GOTAG=1.26.0
# overwrite ADDITIONAL_LDFLAGS to disable static compilation
# this should fix https://github.com/golang/go/issues/13470
ADDITIONAL_LDFLAGS=""
MAKEFILES_VERSION=11.0.0
.DEFAULT_GOAL:=default
GO_ENVIRONMENT=GO111MODULE=on
GO_ENV_VARS=GOPRIVATE=github.com/cloudogu/cesapp CGO_ENABLED=0

include build/make/variables.mk
include build/make/self-update.mk
include build/make/dependencies-gomod.mk
include build/make/build.mk
include build/make/test-common.mk
include build/make/test-unit.mk
include build/make/static-analysis.mk
include build/make/clean.mk
include build/make/package-tar.mk
include build/make/digital-signature.mk
include build/make/mocks.mk

LINT_VERSION=v2.9.0

prepare:
	GO_ENVIRONMENT=GO111MODULE=on,GOOS=linux,GOArch=amd64,CGO_ENABLED=1

default: prepare package


# Overwrites a target with the same name in build/make/mocks.mk which will not work as intended because of doguctl's old project structure
.PHONY: mocks
mocks: ${MOCKERY_BIN} ${MOCKERY_YAML} ## target is used to generate mocks for all interfaces in a project.
	${MOCKERY_BIN}
	@echo "Mocks successfully created."