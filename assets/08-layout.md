# Organisation du code

```text
go-bff-skeleton/
├── cmd/
│   ├── bff/                 le BFF
│   ├── mock-users/          service simulé : utilisateurs
│   └── mock-orders/         service simulé : commandes
├── frontend/                l'application React (Vite, TypeScript)
│   ├── src/
│   ├── embed.go             go:embed du build (dist/)
│   └── dist/                le build (non versionné, sauf .gitkeep)
├── internal/
│   ├── proxy/               la table de routage appliquée
│   ├── compose/             les API propres au BFF
│   ├── web/                 le service de l'application embarquée
│   ├── mock/                les deux services simulés
│   └── platform/            config, logger, health, runner, httpmw, telemetry
├── test/e2e/                les binaires, de bout en bout
├── configs/                 bff.yaml, reference.yaml, bff.env.example
├── deploy/                  configuration du collecteur OpenTelemetry
├── assets/                  ces schémas
├── compose.yaml
├── Dockerfile               Node, Go, distroless
└── Makefile                 make help
```

`internal/platform` vient du squelette go-api-skeleton : les deux squelettes
partagent la même configuration, le même journal, les mêmes sondes et la même
télémétrie.
