import { createContext, useContext, useEffect, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

export interface DetailsSlot {
  target: HTMLElement | null
  open: boolean
  claim: () => () => void
  reveal: () => void
  inspect?: (content: ReactNode, opener: HTMLElement | null) => () => void
  dismissInspection?: () => void
}
export const DetailsSlotContext = createContext<DetailsSlot>({ target: null, open: false, claim: () => () => {}, reveal: () => {} })
export const useDetailsSlot = () => useContext(DetailsSlotContext)

/** Details stay mounted when hidden, so tab changes don't discard local state. */
export function useDetailsPortal(node: ReactNode) {
  const { target, claim } = useDetailsSlot()
  const publishing = target !== null && node != null
  useEffect(() => publishing ? claim() : undefined, [publishing, claim])
  return publishing ? createPortal(node, target) : null
}
