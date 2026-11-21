# Architecture

Le BFF (*Backend For Frontend*) est la seule porte d'entrée de l'application :
le navigateur ne parle qu'à lui, sur une seule origine (pas de CORS), et le BFF
parle aux services internes.

```mermaid
flowchart LR
    browser(["Navigateur<br/>application React"])

    subgraph bff["BFF (un seul binaire)"]
        direction TB
        web["web<br/>application embarquée<br/>(go:embed)"]
        proxy["proxy<br/>table de routage<br/>(configuration)"]
        compose["compose<br/>API propres au BFF<br/>(appels parallèles)"]
    end

    subgraph backends["Services internes"]
        users["users<br/>/api/v1/users"]
        orders["orders<br/>/api/v1/orders"]
    end

    browser -->|"/, /users/1, /assets/…"| web
    browser -->|"/api/users/…, /api/orders/…"| proxy
    browser -->|"/api/dashboard, /api/profiles/{id}"| compose
    proxy --> users
    proxy --> orders
    compose --> users
    compose --> orders
```

| Rôle | Quand l'utiliser |
|------|------------------|
| **proxy** | l'écran consomme une API telle quelle : une ligne de configuration suffit |
| **compose** | l'écran a besoin de données de plusieurs services, ou sous une autre forme : du code, dans le BFF |
| **web** | l'application elle-même, servie par le même binaire |
