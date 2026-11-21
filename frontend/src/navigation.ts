// La navigation de l'application, sur l'API History du navigateur : assez
// pour montrer qu'un lien direct (/users/1) fonctionne, ce qui demande au BFF
// de servir index.html pour les chemins de l'application.
import { useEffect, useState } from 'react'

export function navigate(to: string) {
  window.history.pushState(null, '', to)
  window.dispatchEvent(new PopStateEvent('popstate'))
}

export function usePath(): string {
  const [path, setPath] = useState(window.location.pathname)
  useEffect(() => {
    const onChange = () => setPath(window.location.pathname)
    window.addEventListener('popstate', onChange)
    return () => window.removeEventListener('popstate', onChange)
  }, [])
  return path
}
