import { VisuallyHidden } from 'radix-ui'
import { createContext, useContext, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { msg, useLocale } from '../i18n'
import { Notice } from '../ui/Notice'
import { Popover } from '../ui/Popover'
import { IconException, IconInfo } from './icons'
import { StatusTrigger } from './StatusTrigger'
import styles from './DeskNotices.module.css'

type StatusNotice = {
  id: string
  label: string
  title?: string
  tone?: 'info' | 'warning' | 'error'
  priority?: 'setup' | 'restart'
  children: ReactNode
  actions?: ReactNode
  detail?: ReactNode
}
function createStore() {
  const entries = new Map<symbol, StatusNotice>()
  const listeners = new Set<() => void>()
  let snapshot: StatusNotice[] = []
  const publish = () => {
    // Multiple mounted panes may report the same service. Show it only once.
    const unique = new Map(Array.from(entries.values(), item => [item.id, item]))
    const priority = { error: 0, warning: 1, info: 2 }
    snapshot = [...unique.values()].sort((a, b) => (a.priority === 'restart' ? -1 : a.priority === 'setup' ? 3 : priority[a.tone ?? 'info']) - (b.priority === 'restart' ? -1 : b.priority === 'setup' ? 3 : priority[b.tone ?? 'info']))
    listeners.forEach(listener => listener())
  }
  return {
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener) } },
    snapshot: () => snapshot,
    set(owner: symbol, notice: StatusNotice) { entries.set(owner, notice); publish() },
    remove(owner: symbol) { if (entries.delete(owner)) publish() }
  }
}
const Context = createContext<ReturnType<typeof createStore> | null>(null)
export function DeskNoticesProvider({ children }: { children: ReactNode }) {
  const [store] = useState(createStore)
  return <Context.Provider value={store}>{children}</Context.Provider>
}

/** Shared health and background reads. Form validation and operation results stay at their action. */
export function DeskStatusNotice(notice: StatusNotice) {
  const store = useContext(Context)
  const owner = useRef(Symbol())
  useLayoutEffect(() => { store?.set(owner.current, notice) }, [store, notice])
  useLayoutEffect(() => {
    const key = owner.current
    return () => store?.remove(key)
  }, [store])
  return null
}
const empty: StatusNotice[] = []
const emptySnapshot = () => empty
const noopSubscribe = () => () => {}
export function DeskNotices() {
  useLocale()
  const store = useContext(Context)
  const notices = useSyncExternalStore(store?.subscribe ?? noopSubscribe, store?.snapshot ?? emptySnapshot)
  const [open, setOpen] = useState(false)
  useEffect(() => { if (!notices.length) setOpen(false) }, [notices.length])
  const first = notices[0]
  // Keep one bounded trigger, regardless of how many services need attention.
  const label = first ? notices.length > 1 ? msg('{{notice}} · {{count}} notices', {notice: first.label, count: notices.length}) : first.label : ''
  return <>
    <VisuallyHidden.Root role="status" aria-label={msg('Desk notices')} aria-live="polite" aria-atomic="true">{label}</VisuallyHidden.Root>
    {first && <Popover variant="status" title={notices.length === 1 ? first.title ?? first.label : msg('Desk notices')} align="start" dismissible open={open} onOpenChange={setOpen}
      onCloseAutoFocus={event => {
        if (!store?.snapshot().length) {
          event.preventDefault()
          document.querySelector<HTMLElement>('[data-connection-status]')?.focus()
        }
      }} trigger={<StatusTrigger data-desk-notices data-tone={first.tone ?? 'info'} icon={first.tone && first.tone !== 'info' ? <IconException/> : <IconInfo/>} aria-label={label}>{label}</StatusTrigger>}>
      <div className={styles.list}>{notices.map(notice => <Notice className={styles.notice} key={notice.id} tone={notice.tone} title={notices.length > 1 ? notice.title ?? notice.label : undefined} detail={notice.detail} actions={notice.actions}>{notice.children}</Notice>)}</div>
    </Popover>}
  </>
}
