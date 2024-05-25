test:
	@go test -race -timeout 90s ./... -count=1

test-cover:
	@go test -race -timeout 90s ./... -covermode=atomic -coverprofile=./.cov-report/coverage.out -coverpkg=./... -count=1
	@go tool cover -html=./.cov-report/coverage.out
