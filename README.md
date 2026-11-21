# go-bff-skeleton

Squelette de BFF Go : application React embarquée, proxy vers les API, agrégation.

## Développement

```bash
make help    # les tâches disponibles
make check   # formatage, analyse statique et tests
make lint    # golangci-lint (dans un conteneur)
make vuln    # vulnérabilités des dépendances
```

Organisation :

```text
cmd/       les programmes (un dossier par binaire)
internal/  le code propre à ce module, non importable ailleurs
pkg/       le code réutilisable par d'autres modules (si besoin)
```

Les branches suivent git flow : `develop` pour le travail courant, `main` pour
les versions publiées (`git flow feature start …`, `git flow release start …`).
