import { msg, useLocale } from '../i18n'
import { useRef, useState } from 'react'
import { Button } from '../ui/Button'
import { Input } from '../ui/Input'
import { InspectionRow } from '../ui/InspectionRow'
import { Popover } from '../ui/Popover'
import { matchingItems, type LogicProjection } from './logicModel'
import styles from './PackLogic.module.css'

/** Navigation stays in the reading surface and never opens the Inspector. */
export function PackJumpTo({ model, at, onJump }: { model: LogicProjection; at: string | null; onJump: (pointer: string) => void }) {
  useLocale()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const chosen = useRef<string | null>(null)
  const groups = model.groups.map(group => ({ ...group, items: matchingItems(group, query) })).filter(group => group.items.length)
  return <Popover title={msg("Jump to")} trigger={<Button variant="quiet">{msg("Jump to")}</Button>} open={open}
    onOpenChange={next => { setOpen(next); if (next) { setQuery(''); chosen.current = null } }}
    onCloseAutoFocus={event => {
      if (chosen.current !== null) { event.preventDefault(); onJump(chosen.current); chosen.current = null }
    }}>
    <Input type="search" aria-label={msg("Find section or item")} placeholder={msg("Find section or item…")} value={query}
      className={styles.jumpSearch} onChange={event => setQuery(event.target.value)}
      onKeyDown={event => { if (event.key === 'Enter' && groups[0]?.items[0]) { event.preventDefault(); chosen.current = groups[0].items[0].pointer; setOpen(false) } }} />
    <div className={styles.jumpItems}>{groups.map(group => <section key={group.id} aria-label={group.label}>
      {group.id !== 'applicability' && <h3>{group.label} · {group.items.length}</h3>}
      {group.items.map(item => <InspectionRow key={item.pointer} label={item.label} aria-label={msg("Jump to: {{value0}}", { value0: item.label })}
        current={at === item.pointer} onClick={() => { chosen.current = item.pointer; setOpen(false) }} />)}
    </section>)}</div>
    {!groups.length && <p role="status">{msg("No matching sections or items.")}</p>}
  </Popover>
}
