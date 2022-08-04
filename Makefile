
## --------------------------------------
## Unit tests
## --------------------------------------

.PHONY: unit-test
unit-test:
	go test --cover -v ./... -coverprofile cover.out

.PHONY: unit-test-html
unit-test-html: unit-test
	go tool cover -html=cover.out 