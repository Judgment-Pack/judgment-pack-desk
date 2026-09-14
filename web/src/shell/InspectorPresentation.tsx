import { createContext, useContext, useLayoutEffect } from 'react'

/** A route can own a temporary preview without rewriting the document pane's
 * saved preferences. Releasing it restores the shell's normal Inspector. */
export interface InspectorPresentation {
  title: string
  open: boolean
  onOpenChange: (open: boolean) => void
  width: number
  onResize: (width: number) => void
  onReset: () => void
  minimumMainWidth: number
  maximumWidth: number
  closeOnEscape?: boolean
}

export const InspectorPresentationContext = createContext<(presentation: InspectorPresentation) => () => void>(() => () => {})

export function useInspectorPresentation(presentation: InspectorPresentation | null) {
  const register = useContext(InspectorPresentationContext)
  useLayoutEffect(() => presentation ? register(presentation) : undefined, [register, presentation])
}
