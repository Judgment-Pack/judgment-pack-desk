import { createContext, useCallback, useContext, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { UNSAFE_DataRouterContext, useBlocker, type BlockerFunction } from 'react-router-dom'
import { msg } from '../i18n'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { Input } from '../ui/Input'

export interface UnsavedOptions {
  name?: string
  busy?: boolean
  saveDraft?: () => Promise<void>
  /** Query-only navigation usually retains editors. Override only when it destroys work. */
  shouldBlock?: BlockerFunction
}
interface Entry extends UnsavedOptions { question: string }
interface Request extends Entry { resolve: (leave: boolean) => void; opener: HTMLElement | null }
const Context = createContext<{
  publish: (id: string, entry?: Entry) => void
  confirm: (entry: Entry) => Promise<boolean>
  dirty: boolean
  setTitle: (title: string) => void
  leave: () => Promise<boolean>
} | null>(null)

/** One blocker and one confirmation queue for the whole shell. No form values live here. */
export function UnsavedChangesProvider({ children }: { children: ReactNode }) {
  const entries = useRef(new Map<string, Entry>())
  const [revision, setRevision] = useState(0)
  const [request, setRequest] = useState<Request>()
  const pending = useRef<Request | undefined>(undefined)
  const opener = useRef<HTMLElement | null>(null)
  const [typed, setTyped] = useState(''), [saving, setSaving] = useState(false), [error, setError] = useState('')
  const publish = useCallback((id: string, entry?: Entry) => {
    if (entry) entries.current.set(id, entry)
    else entries.current.delete(id)
    setRevision(n => n + 1)
  }, [])
  const confirm = useCallback((entry: Entry) => {
    // Refuse a competing exit; never overwrite another pending decision.
    if (pending.current || entry.busy) return Promise.resolve(false)
    return new Promise<boolean>(resolve => {
      const next = { ...entry, resolve, opener: document.activeElement instanceof HTMLElement ? document.activeElement : null }
      opener.current = next.opener
      pending.current = next; setTyped(''); setError(''); setRequest(next)
    })
  }, [])
  const finish = useCallback((leave: boolean) => {
    const held = pending.current
    pending.current = undefined; setRequest(undefined); held?.resolve(leave)
  }, [])
  useEffect(() => () => { pending.current?.resolve(false) }, [])
  const dirty = entries.current.size > 0
  const originalTitle = useRef(document.title)
  const [title, setTitle] = useState(document.title.replace(/^\*\s*/, ''))
  useEffect(() => () => { document.title = originalTitle.current }, [])
  useEffect(() => { document.title = `${dirty ? '* ' : ''}${title}` }, [dirty, title])
  const leave = useCallback(async () => {
    const held = [...entries.current.values()]
    if (held.some(entry => entry.busy)) return false
    if (!held.length) return true
    const agreed = await confirm(held.length === 1 ? held[0] : { question: msg('Switching desks will discard unsaved changes in the open editors.') })
    if (agreed && [...entries.current.values()].some(entry => entry.busy)) return false
    if (agreed) { entries.current.clear(); setRevision(n => n + 1) }
    return agreed
  }, [confirm])
  useEffect(() => {
    if (!dirty) return
    const warn = (event: BeforeUnloadEvent) => { if (entries.current.size) { event.preventDefault(); event.returnValue = '' } }
    window.addEventListener('beforeunload', warn)
    return () => { window.removeEventListener('beforeunload', warn) }
  }, [dirty])
  const router = useContext(UNSAFE_DataRouterContext)
  const confirmation = request?.name?.trim() || msg('Yes')
  async function save() {
    if (!request?.saveDraft) return
    setSaving(true); setError('')
    try { await request.saveDraft(); finish(true) }
    catch (e) { setError(e instanceof Error ? e.message : msg('The draft could not be saved. Your changes are still here.')) }
    finally { setSaving(false) }
  }
  return <Context.Provider value={{ publish, confirm, dirty, setTitle, leave }}>
    {router && <NavigationGuard entries={entries.current} revision={revision} confirm={confirm} />}
    {children}
    <Dialog open={!!request} onOpenChange={open => { if (!open && !saving) finish(false) }} title={msg('Unsaved changes')} description={request?.question}
      onCloseAutoFocus={event => { event.preventDefault(); opener.current?.focus() }}
      footer={<DialogActions><Button disabled={saving} onClick={() => finish(false)}>{msg('Keep editing')}</Button>{request?.saveDraft && <Button disabled={saving} onClick={() => void save()}>{saving ? msg('Saving…') : msg('Save draft and leave')}</Button>}<Button variant="danger" disabled={saving || typed.trim() !== confirmation} onClick={() => finish(true)}>{msg('Discard changes')}</Button></DialogActions>}>
      <TypedConfirmation value={typed} onChange={setTyped} confirmation={confirmation} disabled={saving} />
      {error && <p role="alert">{error}</p>}
    </Dialog>
  </Context.Provider>
}
function NavigationGuard({ entries, confirm }: { entries: Map<string, Entry>; revision: number; confirm: (entry: Entry) => Promise<boolean> }) {
  const leaving = useRef(false)
  const affected = useRef<Entry[]>([])
  const blocker = useBlocker(useCallback(args => {
    affected.current = [...entries.values()].filter(entry => entry.shouldBlock ? entry.shouldBlock(args) : args.currentLocation.pathname !== args.nextLocation.pathname)
    return affected.current.length > 0
  }, [entries]))
  useEffect(() => {
    if (blocker.state !== 'blocked' || leaving.current) return
    if (affected.current.some(entry => entry.busy)) { blocker.reset(); return }
    leaving.current = true
    const held = affected.current
    // Save-and-leave is only offered when it saves every affected buffer.
    const entry: Entry = held.length === 1 ? held[0] : { question: msg('Leaving will discard unsaved changes in the open editors.') }
    void confirm(entry).then(leave => {
      if (leave) blocker.proceed(); else blocker.reset()
      leaving.current = false
    })
  }, [blocker, confirm])
  return null
}
export function TypedConfirmation({ confirmation = msg('Yes'), value, onChange, disabled = false }: { confirmation?: string; value: string; onChange: (value: string) => void; disabled?: boolean }) {
  return <Field label={msg('Type {{confirmation}} to confirm.', { confirmation })}>{wiring => <Input {...wiring} autoComplete="off" spellCheck={false} value={value} disabled={disabled} onChange={event => onChange(event.target.value)} />}</Field>
}
export function useConfirmDiscard() {
  const context = useContext(Context)
  return useCallback((question: string, options: UnsavedOptions = {}) => context?.confirm({ question, ...options }) ?? Promise.resolve(false), [context?.confirm])
}
export function useHasUnsavedChanges() { return useContext(Context)?.dirty ?? false }
export function useRegisteredChanges(dirty: boolean, question: string, options: UnsavedOptions = {}) {
  const context = useContext(Context), id = useId()
  const latest = useRef(options); latest.current = options
  const publish = context?.publish
  useEffect(() => {
    publish?.(id, dirty || options.busy ? { question, name: options.name, busy: options.busy,
      ...(options.saveDraft ? { saveDraft: () => latest.current.saveDraft!() } : {}),
      ...(options.shouldBlock ? { shouldBlock: args => latest.current.shouldBlock!(args) } : {}) } : undefined)
    return () => publish?.(id)
  }, [publish, id, dirty, question, options.name, options.busy, !!options.saveDraft, !!options.shouldBlock])
  // A successful write clears the live registration before synchronous navigation.
  return useCallback(() => publish?.(id), [publish, id])
}

export function useConfirmDeskExit() { const context = useContext(Context); return context?.leave ?? (() => Promise.resolve(false)) }
export function useDeskTitle(title: string) {
  const setTitle = useContext(Context)?.setTitle
  useEffect(() => { setTitle?.(title) }, [setTitle, title])
}
