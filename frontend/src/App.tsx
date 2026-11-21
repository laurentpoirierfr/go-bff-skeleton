import { DashboardPage, NotFoundPage, ProfilePage, UsersPage } from './pages'
import { usePath } from './navigation'
import { Link } from './router'

function page(path: string) {
  if (path === '/') return <DashboardPage />
  if (path === '/users') return <UsersPage />
  const m = path.match(/^\/users\/(\d+)$/)
  if (m) return <ProfilePage id={Number(m[1])} />
  return <NotFoundPage />
}

export default function App() {
  const path = usePath()
  return (
    <>
      <header>
        <strong>go-bff-skeleton</strong>
        <nav>
          <Link to="/">Tableau de bord</Link>
          <Link to="/users">Utilisateurs</Link>
        </nav>
      </header>
      <main>{page(path)}</main>
    </>
  )
}
