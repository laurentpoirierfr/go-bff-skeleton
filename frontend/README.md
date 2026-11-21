# Frontend

L'application React (Vite, TypeScript) du BFF. Construite, elle est embarquée
dans le binaire du BFF (`embed.go`, `go:embed all:dist`).

Node n'a pas besoin d'être installé : les commandes tournent dans un conteneur
`node`, depuis la racine du projet.

```bash
make web        # npm ci et npm run build : dist/, puis go build l'embarque
make web-dev    # serveur Vite (http://localhost:5173), rechargement à chaud ;
                # /api est relayé vers le BFF (make run, sur :8080)
make web-lint   # oxlint et la vérification des types
```

L'application appelle le BFF par des chemins relatifs (`/api/...`) : les mêmes
en développement (Vite relaie `/api`) et en production (le BFF sert
l'application et les API sur la même origine, sans CORS).
