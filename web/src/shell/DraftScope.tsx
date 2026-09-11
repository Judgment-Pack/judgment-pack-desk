import { createContext, useCallback, useContext, useEffect, useId, useState, type ReactNode } from 'react'
import { useDirtyGuard } from './useDirtyGuard'

const DraftContext = createContext<((id: string, dirty: boolean) => void) | undefined>(undefined)

/** One route blocker for several independently saved forms. Stores no values. */
export function DraftScope({ children }: { children: ReactNode }) {
  const [dirtyForms, setDirtyForms] = useState<ReadonlySet<string>>(() => new Set())
  const publish = useCallback((id: string, dirty: boolean) => {
    setDirtyForms((held) => {
      if (held.has(id) === dirty) return held
      const next = new Set(held)
      if (dirty) next.add(id)
      else next.delete(id)
      return next
    })
  }, [])
  useDirtyGuard(dirtyForms.size > 0, 'Leave settings and discard your unsaved changes?')
  return <DraftContext.Provider value={publish}>{children}</DraftContext.Provider>
}

/** Publish only whether a form has unsaved work; never copy a draft or key. */
export function useUnsavedChanges(dirty: boolean): void {
  const publish = useContext(DraftContext)
  const id = useId()
  useEffect(() => { publish?.(id, dirty) }, [publish, id, dirty])
  useEffect(() => () => publish?.(id, false), [publish, id])
}
