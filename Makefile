# SRE-Norns / Wyrd library
# Collection of reusable components for your SRE needs


.PHONY: all
all: test

# ==================================================================================== #
# HELPERS
# ==================================================================================== #

## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'

.PHONY: confirm
confirm:
	@echo -n 'Are you sure? [y/N] ' && read ans && [ $${ans:-N} = y ]

.PHONY: no-dirty
no-dirty:
	git diff --exit-code


# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## tidy: format code and tidy modfile
.PHONY: tidy
tidy:
	go fmt ./...
	go mod tidy -v

## verify: Verify go modules and run go vet on the project
.PHONY: verify
verify:
	go mod verify
	go vet ./...

## staticcheck: Run go static-check tool on the code-base
.PHONY: staticcheck
staticcheck:
	go run honnef.co/go/tools/cmd/staticcheck@latest -checks=all,-ST1000,-U1000 ./...

## scan-vuln: Scan for known GO-vulnerabilities
.PHONY: scan-vuln
scan-vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

## audit: run quality control checks
.PHONY: audit
audit: verify staticcheck test # scan-vuln


# ==================================================================================== #
# DEVELOPMENT
# ==================================================================================== #

## test: run all tests
.PHONY: test
test:
	go test -v -race -buildvcs ./...

## test/cover: run all tests and display coverage
.PHONY: test/cover
test/cover:
	go test -v -race -buildvcs -coverprofile=/tmp/coverage.out ./...
	go tool cover -html=/tmp/coverage.out

## clean: remove build artifacts
.PHONY: clean
clean:
	$(RM) -dr ./dist


