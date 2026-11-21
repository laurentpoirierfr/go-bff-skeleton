# go-bff-skeleton

Squelette de **BFF** (*Backend For Frontend*) en Go :

- il **sert l'application React** (Vite, TypeScript), embarquée dans le binaire
  avec `go:embed` ;
- il **relaie les appels `/api/…`** vers les backends, selon une table de
  routage déclarée dans la configuration (`/api/users` →
  `http://users/api/v1/users`) ;
- il **agrège** pour l'interface ce qui demande plusieurs appels
  (`/api/dashboard`).

Deux services simulés (`mock-users`, `mock-orders`) montrent le
fonctionnement.

## Configuration

Le BFF lit, par ordre de priorité croissante :

1. les valeurs par défaut, documentées dans
   [`configs/reference.yaml`](configs/reference.yaml) (générée à partir du code) ;
2. son fichier YAML : `configs/bff.yaml`, ou celui de `-config` (ou
   `CONFIG_FILE`) ;
3. les variables d'environnement : la clé en majuscules, les points remplacés
   par `_` (`proxy.default_timeout` → `PROXY_DEFAULT_TIMEOUT`).

La table de routage est une liste de routes nommées. Une route se déclare dans
le fichier YAML, ou entièrement par l'environnement
(`ROUTES_<NOM>_PREFIX`, `ROUTES_<NOM>_TARGET`, `ROUTES_<NOM>_TIMEOUT`) :

```yaml
routes:
  users:
    prefix: /api/users                          # sous /api/
    target: http://localhost:8091/api/v1/users  # remplace le préfixe
    timeout: 5s                                 # facultatif : proxy.default_timeout
```

```bash
go run ./cmd/bff -print-config     # configuration effective, source de chaque valeur
make config-reference              # régénère reference.yaml et bff.env.example
set -a; . ./configs/bff.env.example; set +a; go run ./cmd/bff   # par l'environnement seul
```

## Développement

```bash
make help    # les tâches disponibles
make check   # formatage, analyse statique et tests
make lint    # golangci-lint (dans un conteneur)
make up      # collecteur OpenTelemetry et Jaeger
make run     # le BFF
```

Les branches suivent git flow : `develop` pour le travail courant, `main` pour
les versions publiées.
