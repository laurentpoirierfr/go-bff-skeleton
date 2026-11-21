# Exécution

```mermaid
flowchart LR
    browser(["navigateur"]) -->|":8080"| bff

    subgraph apps["make up-apps (images distroless)"]
        bff["bff"]
        users["mock-users<br/>:8091"]
        orders["mock-orders<br/>:8092"]
    end

    subgraph obs["make up"]
        otel["collecteur OpenTelemetry<br/>OTLP :4317/:4318"]
        jaeger["Jaeger<br/>UI :16686"]
    end

    bff --> users
    bff --> orders
    bff -.->|traces| otel
    users -.->|traces| otel
    orders -.->|traces| otel
    otel -.-> jaeger
```

Une même trace suit une requête du BFF jusqu'aux services : le BFF transmet
l'en-tête `traceparent` à chaque appel (proxy comme composition).

| Commande | Effet |
|----------|-------|
| `make up` | collecteur et Jaeger, pour développer avec `make run` |
| `make mock-users`, `make mock-orders` | les services simulés (`DELAY=2s`, `FAIL=0.3`) |
| `make up-apps` | tout, depuis les images : http://localhost:8080 |
| `make down` | arrête tout |
