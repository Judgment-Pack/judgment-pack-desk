import {useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type RefObject} from 'react'
import {msg, useLocale} from '../i18n'
import {NoSession} from '../mcp/session'
import {Button} from '../ui/Button'
import {Dialog, DialogActions} from '../ui/Dialog'
import {Disclosure} from '../ui/Disclosure'
import {Field} from '../ui/Field'
import {LoadingState} from '../ui/LoadingState'
import {Notice} from '../ui/Notice'
import {Select} from '../ui/Select'
import {retainedSelectionClient, RetainedReviewError, type ExactRetainedInput, type RetainedReviewView} from './retainedSelection/client'
import {RetainedReviewController, type RetainedReviewState} from './retainedSelection/controller'
import {readRetainedInput} from './retainedSelection/input'
import {ExactText, InputDetails, shortText} from './retainedSelection/InputDetails'
import type {RetainedSelectionSource} from './retainedSelection/wire'
import styles from './DialectsView.module.css'

const idle: RetainedReviewState = Object.freeze({kind: 'idle'})
function MountedInput({input, view, retry}: {input: ExactRetainedInput; view: RetainedReviewView; retry: () => void}) {
 useLocale()
 // This child is removed as soon as the controller drops its mounted input.
 // No projection survives row loading, session end or disposal.
 const generation = useRef(0)
 const projection = useMemo(() => {
  const key = ++generation.current
  try {return {key, reader: readRetainedInput(input, {header: view.header, row: view.rows[input.index]!})}} catch {return undefined}
 }, [input, view])
 return projection ? <InputDetails key={projection.key} reader={projection.reader}/> : <Notice tone="error" actions={<Button onClick={retry}>{msg('Read again')}</Button>}>{msg('This record could not be displayed. Read it again.')}</Notice>
}
/** Inactive direct-import surface. Presentation conveys no action authority. */
export function RetainedRecordReview({source, currentRevision, opener, onClose}: {
 source: RetainedSelectionSource; currentRevision: string; opener: RefObject<HTMLButtonElement | null>; onClose: () => void
}) {
 useLocale()
 const [frozen] = useState<RetainedSelectionSource>(() => ({...source, recordIds: [...source.recordIds], conversionContract: {...source.conversionContract, spec: {...source.conversionContract.spec}}}))
 const owned = useRef<RetainedReviewController | undefined>(undefined)
 const [owner, setOwner] = useState<RetainedReviewController>()
 const lifetime = useRef(0), alive = useRef(false), closed = useRef(false), capturing = useRef(false)
 const [captureState, setCaptureState] = useState<'idle' | 'loading' | 'failed' | 'session-ended'>('idle')
 const subscribe = useCallback((listener: () => void) => owner?.subscribe(listener) ?? (() => {}), [owner])
 const snapshot = useCallback(() => owner?.state ?? idle, [owner])
 const state = useSyncExternalStore(subscribe, snapshot, () => idle)
 const dispose = () => {const controller = owned.current; owned.current = undefined; controller?.dispose()}
 useEffect(() => {
  alive.current = true
  return () => {
   alive.current = false; lifetime.current++; capturing.current = false
   const controller = owned.current; owned.current = undefined; controller?.dispose()
   // StrictMode's next setup must never reuse a terminal owner.
   setOwner(undefined); setCaptureState('idle')
  }
 }, [])
 const close = () => {
  if (closed.current) return
  closed.current = true; alive.current = false; lifetime.current++; capturing.current = false
  dispose(); setOwner(undefined); setCaptureState('idle'); onClose()
 }
 const stale = currentRevision !== frozen.revision
 const prepare = async () => {
  if (!alive.current || closed.current || capturing.current || stale) return
  const controller = owned.current
  if (controller) {
   if (controller.state.kind === 'idle' || controller.state.kind === 'expired' || controller.state.kind === 'unavailable') void controller.prepare(frozen)
   return
  }
  capturing.current = true; setCaptureState('loading')
  const generation = lifetime.current
  try {
   const client = await retainedSelectionClient()
   const next = new RetainedReviewController(client)
   if (!alive.current || closed.current || generation !== lifetime.current) {next.dispose(); return}
   owned.current = next; setOwner(next); setCaptureState('idle')
   void next.prepare(frozen)
  } catch (cause) {
   if (alive.current && !closed.current && generation === lifetime.current) setCaptureState(cause instanceof NoSession || cause instanceof RetainedReviewError && cause.kind === 'session-ended' ? 'session-ended' : 'failed')
  } finally {if (generation === lifetime.current) capturing.current = false}
 }
 const ready = state.kind === 'ready' ? state : undefined
 const index = ready?.mount.kind === 'mounted' ? ready.mount.input.index : ready?.mount.kind === 'loading' || ready?.mount.kind === 'unavailable' ? ready.mount.index : undefined
 const mount = (next: number) => {void owned.current?.mount(next)}
 const busy = captureState === 'loading' || state.kind === 'preparing' || ready?.mount.kind === 'loading'
 const canPrepare = captureState !== 'loading' && captureState !== 'session-ended' && (state.kind === 'idle' || state.kind === 'expired' || state.kind === 'unavailable')
 const row = index === undefined ? undefined : ready?.view.rows[index]
 return <Dialog open onOpenChange={open => {if (!open) close()}} title={msg('Review records')} size="wide" openerRef={opener}
  footer={<DialogActions><Button onClick={close}>{busy ? msg('Cancel') : msg('Close')}</Button>{canPrepare && <Button variant="primary" disabled={stale} onClick={() => void prepare()}>{state.kind === 'idle' && captureState !== 'failed' ? msg('Prepare review') : msg('Prepare review again')}</Button>}</DialogActions>}>
  <div className={styles.stack} aria-busy={busy || undefined}>
   {stale && <Notice tone="warning">{msg('Dialects changed. Close this dialog and review the selection again.')}</Notice>}
   <dl className={styles.detail}><dt>{msg('Source revision')}</dt><dd>{shortText(frozen.revision)}{frozen.revision.length > 120 && <Disclosure title={msg('View full value')}><ExactText text={frozen.revision}/></Disclosure>}</dd><dt>{msg('Selected records')}</dt><dd>{frozen.recordIds.length}</dd></dl>
   {(captureState === 'loading' || state.kind === 'preparing') && <LoadingState>{msg('Preparing review…')}</LoadingState>}
   {(captureState === 'failed' || state.kind === 'unavailable') && <Notice tone="error">{state.kind === 'unavailable' && state.reason === 'resource' ? msg('This review is temporarily unavailable.') : msg('This review could not be prepared.')}</Notice>}
   {state.kind === 'expired' && <Notice tone="warning">{msg('This review is no longer available. Prepare it again to continue.')}</Notice>}
   {(state.kind === 'session-ended' || captureState === 'session-ended') && <Notice tone="warning">{msg('Your session ended. Sign in again to continue.')}</Notice>}
   {ready && <>
    <Field label={msg('Record')}>{wiring => <Select {...wiring} value={index === undefined ? undefined : String(index)} placeholder={msg('Select a record')} options={ready.view.rows.map((item, position) => ({value: String(position), label: `${position + 1} · ${shortText(item.id)}`}))} onValueChange={value => {const position = ready.view.rows.findIndex((_, i) => String(i) === value); if (position >= 0) mount(position)}}/>}</Field>
    <div className={styles.toolbar}><Button disabled={index === undefined || index === 0} onClick={() => {if (index !== undefined) mount(index - 1)}}>{msg('Previous record')}</Button><Button disabled={index === undefined || index + 1 >= ready.view.rows.length} onClick={() => {if (index !== undefined) mount(index + 1)}}>{msg('Next record')}</Button></div>
    {row && <><p role="status" className={styles.note}>{msg('Record')} {index! + 1} · {shortText(row.id)}</p><Disclosure title={msg('Full record identity')} key={index}><ExactText text={row.id}/></Disclosure></>}
    {ready.mount.kind === 'empty' && <p className={styles.note}>{msg('Select a record')}</p>}
    {ready.mount.kind === 'loading' && <LoadingState/>}
    {ready.mount.kind === 'unavailable' && <Notice tone="error" actions={<Button onClick={() => {if (index !== undefined) mount(index)}}>{msg('Read again')}</Button>}>{ready.mount.reason === 'resource' ? msg('This review is temporarily unavailable.') : ready.mount.reason === 'review-not-ready' ? msg('This review is not ready. Read the record again.') : msg('This record could not be read. Read it again.')}</Notice>}
    {ready.mount.kind === 'mounted' && <MountedInput input={ready.mount.input} view={ready.view} retry={() => {if (index !== undefined) mount(index)}}/>}
   </>}
  </div>
 </Dialog>
}
