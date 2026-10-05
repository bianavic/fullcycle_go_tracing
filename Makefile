SERVICES := service-a service-b
BIN_DIR  := bin

.PHONY: fmt vet lint test cover vuln build up down e2e

fmt:
	gofmt -s -w $(foreach s,$(SERVICES),$(s)/)

vet:
	@for s in $(SERVICES); do (cd $$s && go vet ./...) || exit 1; done

lint:
	@for s in $(SERVICES); do (cd $$s && golangci-lint run ./...) || exit 1; done

test:
	@for s in $(SERVICES); do (cd $$s && go test -race ./...) || exit 1; done

cover:
	@for s in $(SERVICES); do (cd $$s && go test -race -cover ./...) || exit 1; done

vuln:
	@for s in $(SERVICES); do (cd $$s && govulncheck ./...) || exit 1; done

build:
	@mkdir -p $(BIN_DIR)
	@for s in $(SERVICES); do (cd $$s && CGO_ENABLED=0 go build -trimpath -o ../$(BIN_DIR)/$$s ./cmd/server) || exit 1; done

up:
	docker compose up --build -d

down:
	docker compose down

e2e:
	./scripts/e2e.sh
