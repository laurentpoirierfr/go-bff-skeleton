# Schémas techniques

Ces schémas décrivent le BFF : son architecture, le chemin d'une requête, le
proxy, la composition, la construction de l'application et l'exécution. Ils
sont écrits en [Mermaid](https://mermaid.js.org) : du texte versionné,
affiché directement par GitHub, et vérifié par `make diagrams`.

| Schéma | Ce qu'il montre |
|--------|-----------------|
| [1. Architecture](01-architecture.md) | le navigateur, le BFF et ses trois rôles, les backends |
| [2. Aiguillage d'une requête](02-routing.md) | qui répond à quel chemin |
| [3. Le proxy](03-proxy.md) | la réécriture, les en-têtes, les erreurs |
| [4. La composition](04-composition.md) | les appels parallèles, le mode dégradé |
| [5. Construire et livrer](05-build.md) | de React au binaire unique, puis à l'image |
| [6. Configuration](06-configuration.md) | la table de routage, les priorités |
| [7. Exécution](07-runtime.md) | compose, les ports, les traces |
| [8. Organisation du code](08-layout.md) | le rôle de chaque répertoire |
