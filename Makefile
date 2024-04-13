test-cover:
	@go test ./... -covermode=atomic -coverprofile=./.cov-report/coverage.out -coverpkg=./... -count=1
	@go tool cover -html=./.cov-report/coverage.out
