# syntax=docker/dockerfile:1
#
# Une image par binaire, à partir du même Dockerfile :
#
#   docker build --build-arg APP=bff         --build-arg VERSION=1.0.0 -t go-bff-skeleton-bff .
#   docker build --build-arg APP=mock-users  -t go-bff-skeleton-mock-users .
#   docker build --build-arg APP=mock-orders -t go-bff-skeleton-mock-orders .
#
# Trois étapes : Node construit l'application React, Go compile le binaire en
# l'embarquant, et l'image finale ne contient que le binaire statique et sa
# configuration par défaut (pas de shell, utilisateur non root).

ARG GO_VERSION=1.27
ARG NODE_VERSION=24

# --- Application React --------------------------------------------------------
FROM docker.io/library/node:${NODE_VERSION}-alpine AS web
WORKDIR /web
# Les dépendances d'abord : cette couche ne change qu'avec package-lock.json.
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# --- Compilation ---------------------------------------------------------------
FROM docker.io/library/golang:${GO_VERSION} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# Le build de l'application, embarqué par frontend/embed.go (go:embed).
COPY --from=web /web/dist ./frontend/dist
ARG APP=bff
ARG VERSION=dev
# CGO_ENABLED=0 : un binaire statique, qui tourne sans libc (distroless/static).
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/version.Version=${VERSION}" \
      -o /out/app ./cmd/${APP}

# --- Exécution -----------------------------------------------------------------
FROM gcr.io/distroless/static-debian13:nonroot
ARG PORT=8080
WORKDIR /app
COPY --from=build /out/app /app/app
# La configuration par défaut du BFF (configs/bff.yaml) ; chaque clé reste
# surchargeable par l'environnement (ROUTES_USERS_TARGET…). Les services
# simulés n'en ont pas besoin.
COPY configs/ /app/configs/
# En conteneur, le journal est en JSON, pour les collecteurs.
ENV LOG_FORMAT=json
EXPOSE ${PORT}
USER nonroot:nonroot
ENTRYPOINT ["/app/app"]
