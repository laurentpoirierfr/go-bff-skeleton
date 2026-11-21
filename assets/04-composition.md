# La composition

Une API de composition appelle plusieurs services **en parallèle** : la
réponse arrive dans le délai du plus lent, et non dans leur somme. Chaque
appel garde le délai de sa route.

```mermaid
sequenceDiagram
    participant B as navigateur
    participant C as BFF (compose)
    participant U as users
    participant O as orders

    B->>C: GET /api/profiles/1
    par en parallèle
        C->>U: GET /api/v1/users/1
        U-->>C: 200 l'utilisateur
    and
        C->>O: GET /api/v1/orders?user_id=1
        O--xC: injoignable
    end
    C-->>B: 200 {user, orders: null, partial: true,<br/>warnings: ["backend orders unavailable"]}
```

Deux politiques, selon ce que l'écran peut montrer :

| API | Si une source manque |
|-----|----------------------|
| `GET /api/dashboard` | **erreur** (502 ou 504) : des totaux faux seraient pires que pas de totaux |
| `GET /api/profiles/{id}` | **réponse dégradée** si les commandes manquent (`partial: true`) : le profil reste utile ; erreur si l'utilisateur manque |
