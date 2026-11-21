// Appels au BFF. Les chemins sont relatifs : en production, l'application est
// servie par le BFF lui-même ; en développement, Vite relaie /api vers lui
// (vite.config.ts).

export type Problem = { title: string; status: number; detail?: string }

export class ApiError extends Error {
  problem: Problem
  constructor(problem: Problem) {
    super(problem.detail ?? problem.title)
    this.problem = problem
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { Accept: 'application/json', ...(init?.body ? { 'Content-Type': 'application/json' } : {}) },
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    // Les erreurs du BFF suivent la RFC 9457 (application/problem+json).
    throw new ApiError({ title: res.statusText, status: res.status, ...body })
  }
  return body as T
}

export type User = { id: number; name: string; email: string }
export type Order = { id: number; user_id: number; product: string; total_cents: number; status: string }
export type UserSummary = { id: number; name: string; order_count: number; total_cents: number }
export type Dashboard = { user_count: number; order_count: number; total_cents: number; top_users: UserSummary[] }
export type Profile = { user: User; orders: Order[] | null; partial: boolean; warnings?: string[] }

export const api = {
  // Composition : le BFF appelle les deux services et assemble la réponse.
  dashboard: () => request<Dashboard>('/api/dashboard'),
  profile: (id: number) => request<Profile>(`/api/profiles/${id}`),
  // Proxy : relayé tel quel vers le service des utilisateurs.
  users: () => request<{ items: User[] }>('/api/users').then((r) => r.items),
  createUser: (u: Omit<User, 'id'>) => request<User>('/api/users', { method: 'POST', body: JSON.stringify(u) }),
}

export const euros = (cents: number) =>
  (cents / 100).toLocaleString('fr-FR', { style: 'currency', currency: 'EUR' })
