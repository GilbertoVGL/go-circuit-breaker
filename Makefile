test:
	@go test ./... -count=1

test-race:
	@go test -race ./... -count=1

test-cover:
	@go test ./... -covermode=atomic -coverprofile=./.cov-report/coverage.out -coverpkg=./... -count=1
	@go tool cover -html=./.cov-report/coverage.out
