# go-bff-skeleton : tâches courantes. « make help » les liste.
GOLANGCI_IMAGE := docker.io/golangci/golangci-lint:v2.15.0
# Podman sans root : keep-id garde l'identité de l'utilisateur dans le conteneur.
CONTAINER_USER := $(if $(shell docker --version 2>/dev/null | grep -i podman),--userns=keep-id,--user $(shell id -u):$(shell id -g))

.PHONY: help build test check lint vuln cover clean config-reference run up down

## help : liste les cibles
help:
	@grep -h '^## ' $(MAKEFILE_LIST) | sed 's/^## //'

## build : compile les programmes dans bin/
build:
	go build -o bin/ ./cmd/...

## test : lance tous les tests
test:
	go test ./...

## config-reference : régénère configs/reference.yaml et configs/bff.env.example à partir du code
config-reference:
	UPDATE_CONFIG_REFERENCE=1 go test -count=1 -run 'TestReferenceUpToDate|TestEnvExample' ./internal/platform/config

## up : démarre le collecteur OpenTelemetry et Jaeger (http://localhost:16686)
up:
	docker compose up -d --wait

## down : arrête les conteneurs et supprime leurs données
down:
	docker compose down -v

## run : lance le BFF avec configs/bff.yaml
run:
	go run ./cmd/bff -config configs/bff.yaml

## check : formatage, analyse statique et tests (avant chaque commit)
check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test -short ./...

## lint : golangci-lint, dans son conteneur (rien à installer)
lint:
	docker run --rm --volume "$(CURDIR):/app" --workdir /app $(CONTAINER_USER) -e HOME=/tmp \
		-e GOFLAGS=-buildvcs=false $(GOLANGCI_IMAGE) golangci-lint run ./...

## vuln : vulnérabilités connues des dépendances
vuln:
	go tool govulncheck ./...

## cover : couverture des tests, en HTML
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

## clean : supprime les fichiers produits
clean:
	rm -rf bin coverage.out coverage.html
