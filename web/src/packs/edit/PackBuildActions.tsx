import { msg } from '../../i18n'
import { Button } from '../../ui/Button'
import { Popover, PopoverClose } from '../../ui/Popover'
import { useEditing } from './editingContext'
import { addElement, setRawJson } from './writes'
import { valueAt } from '../pointers'
import { NEW_NODE } from './conditionOps'

/** Add through the same span-preserving writer and undo stack as the full form. */
export function PackBuildActions({onSelect, editorOpen, onToggleEditor}: {onSelect: (pointer: string) => void; editorOpen: boolean; onToggleEditor: () => void}) {
  const session = useEditing()
  return <>
    <Button variant="quiet" aria-pressed={editorOpen} onClick={onToggleEditor}>{msg('Editor')}</Button>
    <Popover title={msg('Add item')} trigger={<Button>{msg('Add item')}</Button>}>
      {(['rules', 'exceptions', 'outcomes', 'evidenceRequirements', 'sources'] as const).map(group => <PopoverClose key={group}><Button variant="quiet" onClick={() => {
        const at = `/${group}`, current = valueAt(session.buffer.index.value, at)
        if (current !== undefined && !Array.isArray(current)) return
        const index = Array.isArray(current) ? current.length : 0, next = `${at}/${index}`
        const existing = new Set(Array.isArray(current) ? current.map(item => item?.id) : [])
        const prefix = group === 'rules' ? 'rule' : group === 'exceptions' ? 'exception' : group === 'outcomes' ? 'outcome' : group === 'sources' ? 'source' : 'evidence'
        let id = prefix, suffix = 2; while (existing.has(id)) id = `${prefix}-${suffix++}`
        const item = group === 'rules' ? {id, when: NEW_NODE, outcome: '', onUnknown: 'escalate'}
          : group === 'exceptions' ? {id, when: NEW_NODE, effect: 'escalate', onUnknown: 'escalate'}
          : group === 'outcomes' ? {id, label: ''} : group === 'evidenceRequirements' ? {id, description: '', required: false}
          : {id, title: '', locator: {kind: 'uri', value: ''}, citation: {location: '', excerpt: ''}}
        const json = JSON.stringify(item)
        session.write(buffer => addElement(current === undefined ? setRawJson(buffer, at, '[]') : buffer, at, json))
        onSelect(next)
      }}>{group === 'rules' ? msg('Rule') : group === 'exceptions' ? msg('Special case') : group === 'outcomes' ? msg('Outcome') : group === 'evidenceRequirements' ? msg('Evidence requirement') : msg('Source')}</Button></PopoverClose>)}
    </Popover>
  </>
}
