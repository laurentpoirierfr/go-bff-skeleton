// Link navigue sans recharger la page (voir navigation.ts).
import type { MouseEvent, ReactNode } from 'react'
import { navigate } from './navigation'

export function Link({ to, children }: { to: string; children: ReactNode }) {
  const onClick = (e: MouseEvent) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return // nouvel onglet : laisser faire
    e.preventDefault()
    navigate(to)
  }
  return (
    <a href={to} onClick={onClick}>
      {children}
    </a>
  )
}
