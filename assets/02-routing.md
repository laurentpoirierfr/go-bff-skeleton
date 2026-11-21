# Aiguillage d'une requête

Le routeur de la bibliothèque standard (`http.ServeMux`) choisit le motif le
plus précis : une API de composition l'emporte sur la réponse 404 de `/api/`,
qui l'emporte sur l'application.

```mermaid
flowchart TD
    req(["requête"]) --> health{"/health/… ?"}
    health -->|oui| probes["sondes live et ready"]
    health -->|non| comp{"GET /api/dashboard<br/>GET /api/profiles/{id} ?"}
    comp -->|oui| compose["composition"]
    comp -->|non| route{"commence par le<br/>préfixe d'une route ?"}
    route -->|oui| proxy["proxy vers le backend"]
    route -->|non| api{"/api/… ?"}
    api -->|oui| notfound["404 problem+json<br/>(jamais l'application)"]
    api -->|non| file{"fichier du build ?"}
    file -->|oui| asset["le fichier<br/>(cache, gzip, ETag)"]
    file -->|non| ext{"le chemin a une<br/>extension ?"}
    ext -->|oui| missing["404<br/>(un script absent n'est pas une page)"]
    ext -->|non| index["index.html<br/>(le routeur React prend la suite)"]
```

| Chemin | Réponse | Cache |
|--------|---------|-------|
| `/`, `/users/1` | `index.html`, avec la politique de sécurité (CSP) | jamais (`no-cache`) |
| `/assets/index-3f2a1b.js` | le fichier, compressé si le client l'accepte | un an (`immutable`) : le nom porte une empreinte |
| `/api/users/42` | le backend, par le proxy | celui du backend |
| `/api/inconnu` | 404 `application/problem+json` | — |
