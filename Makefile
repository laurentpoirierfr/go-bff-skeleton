# go-bff-skeleton : tâches courantes. « make help » les liste.
GOLANGCI_IMAGE := docker.io/golangci/golangci-lint:v2.15.0
# Podman sans root : keep-id garde l'identité de l'utilisateur dans le conteneur.
CONTAINER_USER := $(if $(shell docker --version 2>/dev/null | grep -i podman),--userns=keep-id,--user $(shell id -u):$(shell id -g))

.PHONY: help build test check lint vuln cover clean config-reference run up down mock-users mock-orders web web-dev web-lint images up-apps e2e

## help : liste les cibles
help:
	@grep -h '^## ' $(MAKEFILE_LIST) | sed 's/^## //'

## build : compile les programmes dans bin/ (lancer make web avant, pour embarquer l'application)
build:
	go build -o bin/ ./cmd/...

## test : lance tous les tests
test:
	go test ./...

## config-reference : régénère configs/reference.yaml et configs/bff.env.example à partir du code
config-reference:
	UPDATE_CONFIG_REFERENCE=1 go test -count=1 -run 'TestReferenceUpToDate|TestEnvExample' ./internal/platform/config

# La version des images : le dernier tag, et le commit s'il y en a d'autres depuis.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE ?= go-bff-skeleton

## images : construit les images du BFF et des deux services simulés (IMAGE, VERSION)
images:
	docker build --build-arg APP=bff --build-arg VERSION=$(VERSION) -t $(IMAGE)-bff:$(VERSION) .
	docker build --build-arg APP=mock-users --build-arg PORT=8091 --build-arg VERSION=$(VERSION) -t $(IMAGE)-mock-users:$(VERSION) .
	docker build --build-arg APP=mock-orders --build-arg PORT=8092 --build-arg VERSION=$(VERSION) -t $(IMAGE)-mock-orders:$(VERSION) .

## up : démarre le collecteur OpenTelemetry et Jaeger (http://localhost:16686)
up:
	docker compose up -d --wait

## up-apps : comme up, plus le BFF et les deux services simulés, construits depuis le Dockerfile (http://localhost:8080)
up-apps:
	docker compose --profile apps up -d --build --wait

## down : arrête tout et supprime les données
down:
	docker compose --profile apps down -v

## run : lance le BFF avec configs/bff.yaml
run:
	go run ./cmd/bff -config configs/bff.yaml

## mock-users, mock-orders : les services simulés (:8091, :8092) ; DELAY=2s, FAIL=0.3 pour simuler un backend lent ou en panne
DELAY ?= 0s
FAIL ?= 0
mock-users:
	go run ./cmd/mock-users -address :8091 -delay $(DELAY) -fail $(FAIL) -telemetry-endpoint http://localhost:4318
mock-orders:
	go run ./cmd/mock-orders -address :8092 -delay $(DELAY) -fail $(FAIL) -telemetry-endpoint http://localhost:4318

# Le frontend se construit dans un conteneur Node : rien à installer.
NODE_IMAGE := docker.io/library/node:24-alpine
NODE = docker run --rm $(CONTAINER_USER) -e HOME=/tmp -e npm_config_cache=/tmp/.npm \
	--volume "$(CURDIR)/frontend:/app" --workdir /app

## web : construit l'application React (frontend/dist), qu'embarque ensuite le BFF
web:
	$(NODE) $(NODE_IMAGE) sh -c 'npm ci --no-audit --no-fund && npm run build'

## web-dev : serveur Vite (http://localhost:5173) avec rechargement à chaud ; /api vers le BFF (make run)
web-dev:
	$(NODE) -it --network host $(NODE_IMAGE) sh -c 'npm ci --no-audit --no-fund && npm run dev'

## web-lint : oxlint et vérification des types du frontend
web-lint:
	$(NODE) $(NODE_IMAGE) sh -c 'npm ci --no-audit --no-fund && npm run lint && npx tsc -b'

## e2e : teste les binaires compilés (BFF et services simulés), lancés comme des processus ; make web avant, pour tester aussi l'application
e2e:
	go test -count=1 -v ./test/e2e

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
