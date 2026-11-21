import { useCallback, useEffect, useState } from 'react'
import { ApiError } from './api'

// useApi charge une ressource, et expose son état : chargement, données ou
// erreur, et une fonction pour recharger.
export function useApi<T>(load: () => Promise<T>, deps: unknown[]) {
  const [state, setState] = useState<{ data?: T; error?: ApiError; loading: boolean }>({ loading: true })
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const run = useCallback(load, deps)
  const reload = useCallback(() => {
    setState((s) => ({ ...s, loading: true }))
    run()
      .then((data) => setState({ data, loading: false }))
      .catch((error: ApiError) => setState({ error, loading: false }))
  }, [run])
  useEffect(reload, [reload])
  return { ...state, reload }
}
