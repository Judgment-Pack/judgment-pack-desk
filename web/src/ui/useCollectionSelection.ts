import { useEffect, useRef, useState } from 'react'

/** Selection covers the current matching inventory, not just its displayed page. */
export function useCollectionSelection<T>(items: readonly T[], key: (item: T) => string, scope = '') {
  const keys = items.map(key), inventory = JSON.stringify(keys), allowed = new Set(keys)
  const [state, setState] = useState<{scope: string; ids: string[]}>({scope, ids: []})
  const anchor = useRef<{scope:string;id:string} | undefined>(undefined)
  const header = useRef<HTMLInputElement>(null)
  const ids = state.scope === scope ? state.ids.filter(id => allowed.has(id)) : []
  const selected = new Set(ids)
  useEffect(() => {
    const current = new Set<string>(JSON.parse(inventory))
    setState(before => {
      const retained = before.scope === scope ? before.ids.filter(id => current.has(id)) : []
      return before.scope === scope && retained.length === before.ids.length ? before : {scope, ids: retained}
    })
    if (!anchor.current || anchor.current.scope !== scope || !current.has(anchor.current.id)) anchor.current = undefined
  }, [inventory, scope])
  function clear() { anchor.current = undefined; setState({scope, ids: []}) }
  function toggle(id: string, range = false) {
    if (!allowed.has(id)) return
    const start = range && anchor.current?.scope === scope ? keys.indexOf(anchor.current.id) : -1
    const targets = start < 0 ? [id] : keys.slice(Math.min(start, keys.indexOf(id)), Math.max(start, keys.indexOf(id)) + 1)
    anchor.current = {scope,id}
    setState(before => {
      const next = new Set(before.scope === scope ? before.ids.filter(value => allowed.has(value)) : [])
      const remove = next.has(id)
      for (const value of targets) { if (remove) next.delete(value); else next.add(value) }
      return {scope, ids: [...next]}
    })
  }
  const all = keys.length > 0 && selected.size === allowed.size
  function toggleAll() { anchor.current = undefined; setState({scope, ids: all ? [] : [...allowed]}) }
  return { selectedRows: items.filter(item => selected.has(key(item))), count: selected.size,
    all, mixed: selected.size > 0 && !all, empty: !keys.length,
    has: (id: string) => selected.has(id), toggle, toggleAll, clear, header }
}
export type CollectionSelection = Omit<ReturnType<typeof useCollectionSelection>, 'selectedRows'>
