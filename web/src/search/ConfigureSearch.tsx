import {LoadingState} from '../ui/LoadingState'
import { useRef, useState, type RefObject } from 'react'
import { msg, useLocale } from '../i18n'
import { Button, ButtonLink } from '../ui/Button'
import { Dialog, DialogActions } from '../ui/Dialog'
import { Notice } from '../ui/Notice'
import { SearchEditor } from './WebSearchSettings'
import { useSearchConnections, type SearchConnection } from './connections'
import styles from './WebSearchSettings.module.css'

/** Contextual entry to the same credential editor used in Admin. No route change,
 * no credentials in chat, and choosing a connection changes only this chat. */
export function ConfigureSearch({ open, onClose, onSelect, selected, repair = false, openerRef, fallbackRef }: {
 open: boolean; onClose: () => void; onSelect: (id: string) => void; selected?: string; repair?: boolean
 openerRef: RefObject<HTMLElement | null>; fallbackRef?: RefObject<HTMLElement | null>
}) {
 useLocale()
 const close = useRef(onClose)
 return <Dialog open={open} onOpenChange={next => { if (!next) close.current() }} title={msg('Web search')} openerRef={openerRef}
  onCloseAutoFocus={fallbackRef ? event => { event.preventDefault(); (openerRef.current?.isConnected ? openerRef.current : fallbackRef.current)?.focus({preventScroll:true}) } : undefined}>
  {open && <SearchSetup selected={selected} repair={repair} onClose={onClose} onSelect={onSelect} registerClose={action => { close.current = action }}/>}
 </Dialog>
}
function SearchSetup({ selected, repair, onClose, onSelect, registerClose }: {
 selected?: string; repair: boolean; onClose: () => void; onSelect: (id: string) => void; registerClose: (close: () => void) => void
}) {
 const connections = useSearchConnections()
 const [editing, setEditing] = useState<SearchConnection | null | undefined>()
 const data = connections.data
 const original = editing !== undefined ? editing : repair ? data?.connections.find(c => c.id === selected) : undefined
 const adding = data?.connections.length === 0 || editing === null
 registerClose(onClose)
 if (connections.loading) return <LoadingState>{msg('Loading connections…')}</LoadingState>
 if (connections.isError) return <Notice tone="error" actions={<Button onClick={() => void connections.refetch()}>{msg('Retry')}</Button>}>{msg('Search connections could not be loaded.')}</Notice>
 if (!connections.available) return <><p>{msg('Web search requires an updated local gateway.')}</p><DialogActions><Button onClick={onClose}>{msg('Cancel')}</Button><ButtonLink to="/admin#gateway" onClick={onClose}>{msg('Manage gateway')}</ButtonLink></DialogActions></>
 if (!data) return null
 if (adding || original) return <SearchEditor key={original?.id ?? 'new'} original={original ?? null} providers={data.providers} timeout={data.timeout}
  onClose={onClose} onSaved={onSelect} onTested={onSelect} registerClose={registerClose}/>
 return <div className={styles.settings}>
  <p className={styles.caption}>{msg('Choose a search connection for this chat. Your desk default stays unchanged.')}</p>
  <div className={styles.connections}>{data.connections.map(connection => <div className={styles.connectionRow} key={connection.id}>
   <div><strong>{connection.name}</strong><p>{data.providers.find(provider => provider.id === connection.provider)?.name}</p></div>
   <Button variant="quiet" aria-label={msg('Manage {{name}}', {name: connection.name})} onClick={() => setEditing(connection)}>{msg('Manage')}</Button>
   <Button aria-label={msg('Use {{name}}', {name: connection.name})} onClick={() => onSelect(connection.id)}>{msg('Use connection')}</Button>
  </div>)}</div>
  <DialogActions><Button variant="quiet" onClick={() => setEditing(null)}>{msg('Add connection')}</Button><Button onClick={onClose}>{msg('Cancel')}</Button></DialogActions>
 </div>
}
