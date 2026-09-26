import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { msg, useLocale } from '../i18n'
import { useInspectorControls } from '../shell/InspectorSlot'
import { IconClose, IconDetails, IconChevronRight } from '../shell/icons'
import { Button } from '../ui/Button'
import { Tooltip } from '../ui/Tooltip'
import styles from './AssistantReference.module.css'

export interface AssistantReference { label: string; text: string; onOpen: () => void }
type References = { scope: string; items: Record<string, AssistantReference>; pending?: AssistantReference }
const ReferenceContext = createContext<{
  items: Record<string, AssistantReference>
  register: (id: string) => () => void
  attach: (reference: AssistantReference) => void
  remove: (id: string, reference: AssistantReference) => void
} | null>(null)

export function AssistantReferenceProvider({ children }: { children: ReactNode }) {
  const { pathname: scope } = useLocation()
  const [state, setState] = useState<References>({ scope, items: {} })
  const [active, setActive] = useState<{ scope: string; id: string } | null>(null)
  useEffect(() => { setState(previous => previous.scope === scope ? previous : { scope, items: {} }) }, [scope])
  const register = useCallback((id: string) => {
    const owner = { scope, id }; setActive(owner)
    setState(previous => previous.scope === scope && previous.pending ? { scope, items: { ...previous.items, [id]: previous.pending } } : previous)
    return () => setActive(current => current === owner ? null : current)
  }, [scope])
  const attach = useCallback((reference: AssistantReference) => setState(previous => {
    const items = previous.scope === scope ? previous.items : {}
    return active?.scope === scope ? { scope, items: { ...items, [active.id]: reference } } : { scope, items, pending: reference }
  }), [scope, active])
  const remove = useCallback((id: string, reference: AssistantReference) => setState(previous => {
    if (previous.scope !== scope || previous.items[id] !== reference) return previous
    const items = { ...previous.items }; delete items[id]; return { ...previous, items }
  }), [scope])
  const value = useMemo(() => ({ items: state.scope === scope ? state.items : {}, register, attach, remove }), [state, scope, register, attach, remove])
  return <ReferenceContext.Provider value={value}>{children}</ReferenceContext.Provider>
}

export function useChatReference(id: string, enabled: boolean) {
  const context = useContext(ReferenceContext)
  const register = context?.register
  useLayoutEffect(() => enabled ? register?.(id) : undefined, [register, id, enabled])
  const reference = enabled ? context?.items[id] : undefined
  return { reference, remove: () => { if (reference) context?.remove(id, reference) } }
}

export function DetailsWithAssistant({ reference, children }: { reference: AssistantReference; children: ReactNode }) {
  useLocale()
  const context = useContext(ReferenceContext)
  const assistant = useInspectorControls()
  if (!context) return children
  return <div className={styles.workspace} data-detail-workspace>
    <div className={styles.body}>{children}</div>
    <div className={styles.footer}><Button variant="secondary" onClick={() => {
      context.attach(reference); assistant.reveal()
      requestAnimationFrame(() => document.querySelector<HTMLTextAreaElement>('#desk-inspector textarea')?.focus({ preventScroll: true }))
    }}>{msg('Ask Assistant about this')}</Button></div>
  </div>
}

export function ReferenceChip({ reference, onRemove }: { reference: AssistantReference; onRemove: () => void }) {
  useLocale()
  return <div className={styles.chip}>
    <Tooltip content={msg('Open details: {{name}}', { name: reference.label })}><button className={styles.open} type="button" onClick={reference.onOpen} aria-label={msg('Open details: {{name}}', { name: reference.label })}><IconDetails /><span>{reference.label}</span><IconChevronRight /></button></Tooltip>
    <Tooltip content={msg('Remove reference')}><button className={styles.remove} type="button" aria-label={msg('Remove reference')} onClick={onRemove}><IconClose /></button></Tooltip>
  </div>
}
