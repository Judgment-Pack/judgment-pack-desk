import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

interface NavigationSlot {
  target: HTMLDivElement | null
  publish: (node: HTMLDivElement | null) => void
}

const Context = createContext<NavigationSlot | null>(null)

/** The active settings page owns its links; the shell owns their location. */
export function SettingsNavigationProvider({ children }: { children: ReactNode }) {
  const [target, publish] = useState<HTMLDivElement | null>(null)
  const value = useMemo(() => ({ target, publish }), [target])
  return <Context.Provider value={value}>{children}</Context.Provider>
}

export function SettingsNavigationTarget({ onNavigate }: { onNavigate?: () => void }) {
  const slot = useContext(Context)
  useEffect(() => {
    const target = slot?.target
    if (target === null || target === undefined || onNavigate === undefined) return
    // Portal events follow the React owner tree. A native listener on the
    // target dismisses the navigation drawer after a link has navigated.
    const clicked = (event: MouseEvent) => {
      if (event.target instanceof Element && event.target.closest('a')) queueMicrotask(onNavigate)
    }
    target.addEventListener('click', clicked)
    return () => target.removeEventListener('click', clicked)
  }, [slot?.target, onNavigate])
  return <div className="desk-settings-navigation" ref={slot?.publish} />
}

export function useSettingsNavigation() {
  const slot = useContext(Context)
  return {
    inSidebar: slot !== null,
    render: (navigation: ReactNode) => {
      if (slot === null) return navigation
      return slot.target === null ? null : createPortal(navigation, slot.target)
    }
  }
}
