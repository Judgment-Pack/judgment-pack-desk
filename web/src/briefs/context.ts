import { createContext, useContext, useLayoutEffect, useMemo } from 'react'
import type { Snapshot } from './model'
export interface BriefSubject { id: string; path: string; title: string; owner?: string; caseId?: string; snapshot?: Snapshot; unavailable?: string }
export const BriefContext = createContext<(subject: BriefSubject) => () => void>(() => () => undefined)
export function useBriefSubject(subject: BriefSubject | null) {
  const register = useContext(BriefContext)
  const serialized = JSON.stringify(subject)
  const stable = useMemo(() => JSON.parse(serialized) as BriefSubject | null, [serialized])
  useLayoutEffect(() => stable ? register(stable) : undefined, [register, stable])
}
