# Construire et livrer

L'application React est construite par Vite, puis **embarquée** dans le
binaire Go (`go:embed`) : un seul fichier à déployer, sans serveur web à côté.

```mermaid
flowchart LR
    src["frontend/src<br/>React, TypeScript"] -->|"npm run build<br/>(Vite)"| dist["frontend/dist<br/>index.html, assets/…"]
    dist -->|"go:embed all:dist<br/>(frontend/embed.go)"| bin["binaire bff"]
    go["cmd/bff, internal/…"] -->|go build| bin
    bin --> image["image distroless<br/>non root"]
```

```mermaid
flowchart TB
    subgraph docker["Dockerfile : trois étapes"]
        direction LR
        n["node:24-alpine<br/>npm ci, npm run build"] --> g["golang:1.27<br/>go build (dist embarqué)"] --> r["distroless/static:nonroot<br/>le binaire et configs/"]
    end
```

| Commande | Effet |
|----------|-------|
| `make web` | construit `frontend/dist` (dans un conteneur Node) |
| `make build` | compile les binaires, en embarquant le dernier build |
| `make web-dev` | Vite sur :5173, rechargement à chaud, `/api` relayé vers le BFF |
| `make images` | les images du BFF et des services simulés |

Sans build, `frontend/dist/.gitkeep` permet quand même de compiler : le BFF
sert alors une page qui dit de lancer `make web`.
