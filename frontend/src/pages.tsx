import { useState, type FormEvent } from 'react'
import { api, euros, ApiError } from './api'
import { Link } from './router'
import { useApi } from './useApi'

function Failure({ error, retry }: { error: ApiError; retry: () => void }) {
  return (
    <div className="error" role="alert">
      <strong>
        {error.problem.status} {error.problem.title}
      </strong>
      <p>{error.message}</p>
      <button onClick={retry}>Réessayer</button>
    </div>
  )
}

// Le tableau de bord : une seule requête, composée par le BFF à partir de deux
// services.
export function DashboardPage() {
  const { data, error, loading, reload } = useApi(api.dashboard, [])
  if (error) return <Failure error={error} retry={reload} />
  if (!data) return <p aria-busy="true">Chargement…</p>
  return (
    <section>
      <h1>Tableau de bord</h1>
      <p className="hint">
        <code>GET /api/dashboard</code> : le BFF appelle les utilisateurs et les commandes en parallèle, et compose
        cette vue. {loading && '(actualisation…)'}
      </p>
      <div className="cards">
        <div className="card">
          <span>Utilisateurs</span>
          <strong>{data.user_count}</strong>
        </div>
        <div className="card">
          <span>Commandes</span>
          <strong>{data.order_count}</strong>
        </div>
        <div className="card">
          <span>Chiffre d'affaires</span>
          <strong>{euros(data.total_cents)}</strong>
        </div>
      </div>
      <h2>Meilleurs clients</h2>
      <table>
        <thead>
          <tr>
            <th>Client</th>
            <th>Commandes</th>
            <th>Montant</th>
          </tr>
        </thead>
        <tbody>
          {data.top_users.map((u) => (
            <tr key={u.id}>
              <td>
                <Link to={`/users/${u.id}`}>{u.name}</Link>
              </td>
              <td>{u.order_count}</td>
              <td>{euros(u.total_cents)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

// Les utilisateurs : la liste et la création passent par le proxy, telles
// quelles, vers le service des utilisateurs.
export function UsersPage() {
  const { data, error, reload } = useApi(api.users, [])
  const [form, setForm] = useState({ name: '', email: '' })
  const [createError, setCreateError] = useState<string>()
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setCreateError(undefined)
    try {
      await api.createUser(form)
      setForm({ name: '', email: '' })
      reload()
    } catch (err) {
      setCreateError((err as Error).message)
    }
  }
  if (error) return <Failure error={error} retry={reload} />
  return (
    <section>
      <h1>Utilisateurs</h1>
      <p className="hint">
        <code>GET /api/users</code> et <code>POST /api/users</code> : relayés par le proxy vers{' '}
        <code>/api/v1/users</code> du service des utilisateurs.
      </p>
      <ul className="list">
        {data?.map((u) => (
          <li key={u.id}>
            <Link to={`/users/${u.id}`}>{u.name}</Link> <span className="muted">{u.email}</span>
          </li>
        ))}
      </ul>
      <form onSubmit={submit} className="form">
        <h2>Nouvel utilisateur</h2>
        <input placeholder="Nom" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
        <input
          placeholder="Courriel"
          type="email"
          value={form.email}
          onChange={(e) => setForm({ ...form, email: e.target.value })}
          required
        />
        <button type="submit">Créer</button>
        {createError && <p className="error">{createError}</p>}
      </form>
    </section>
  )
}

// Le profil : composé par le BFF ; si les commandes manquent, la réponse est
// dégradée mais le profil reste affiché.
export function ProfilePage({ id }: { id: number }) {
  const { data, error, reload } = useApi(() => api.profile(id), [id])
  if (error) return <Failure error={error} retry={reload} />
  if (!data) return <p aria-busy="true">Chargement…</p>
  return (
    <section>
      <p>
        <Link to="/users">← Utilisateurs</Link>
      </p>
      <h1>{data.user.name}</h1>
      <p className="muted">{data.user.email}</p>
      <p className="hint">
        <code>GET /api/profiles/{id}</code> : l'utilisateur et ses commandes, composés par le BFF.
      </p>
      {data.partial && (
        <div className="warning" role="status">
          Profil incomplet : {data.warnings?.join(' ; ')}
        </div>
      )}
      {data.orders && (
        <table>
          <thead>
            <tr>
              <th>Commande</th>
              <th>Produit</th>
              <th>Statut</th>
              <th>Montant</th>
            </tr>
          </thead>
          <tbody>
            {data.orders.map((o) => (
              <tr key={o.id}>
                <td>#{o.id}</td>
                <td>{o.product}</td>
                <td>{o.status}</td>
                <td>{euros(o.total_cents)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

export function NotFoundPage() {
  return (
    <section>
      <h1>Page introuvable</h1>
      <Link to="/">Retour au tableau de bord</Link>
    </section>
  )
}
