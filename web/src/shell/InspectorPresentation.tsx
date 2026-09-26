import { createContext, useContext, useLayoutEffect, type RefObject } from 'react'

/** A route can own a temporary preview without rewriting the document pane's
 * saved preferences. Releasing it restores the shell's normal Inspector. */
export interface InspectorPresentation {
  workspaceTools?: boolean
  /** Optional document identity retained in the expanded pane header. */
  contextTitle?: string
  title: string
  available?: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  width: number
  onResize: (width: number) => void
  onReset: () => void
  minimumMainWidth: number
  maximumWidth: number
  closeOnEscape?: boolean
  /** A temporary pane can return focus to its own setup or preview control. */
  restoreFocusRef?: RefObject<HTMLElement | null>
}

export const InspectorPresentationContext = createContext<(presentation: InspectorPresentation) => () => void>(() => () => {})

export function useInspectorPresentation(presentation: InspectorPresentation | null) {
  const register = useContext(InspectorPresentationContext)
  useLayoutEffect(() => presentation ? register(presentation) : undefined, [register, presentation])
}
