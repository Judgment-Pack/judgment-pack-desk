import { Message } from '../i18n/Message'
import { msg, useLocale } from '../i18n'
import { useMemo, useState } from 'react'
import type { PackDocument } from '../mcp/types'
import { CodeArea } from '../ui/CodeArea'
import { Tabs } from '../ui/Tabs'
import { BuilderSplit } from '../builders/BuilderSplit'
import { agreesWithParse } from './documentText'
import { PackDocumentView } from './document/PackDocumentView'
import { isRecord } from './document/MisshapenMember'
import { EditingContext, declaredIds, type PendingText } from './edit/editingContext'
import { buffered } from './edit/writes'
import { SelectedLogicEditor } from './edit/SelectedLogicEditor'
import { PackBuildActions } from './edit/PackBuildActions'
import { projectLogic, selectedItem } from './logicModel'
import { useLogicState } from './logicState'
import { PackLogic } from './PackLogic'
import styles from './DraftPackEditor.module.css'

/** Creation and file editing share the same item forms and bottom editor. */
export function DraftPackEditor({ text, onChange, pending, hold }: {
  text: string; onChange: (text: string) => void
  pending: ReadonlyMap<string, PendingText>
  hold: (pointer: string, draft: PendingText | null) => void
}) {
  useLocale()
  const [tab, setTab] = useState('build'), [at, setAt] = useState<string | null>(null), [open, setOpen] = useState(false)
  const logic = useLogicState()
  const read = useMemo(() => buffered(text), [text]), doc = read.index.value
  const form = isRecord(doc) && agreesWithParse(text, read.index).length === 0
  const model = useMemo(() => form ? projectLogic(doc as unknown as PackDocument) : undefined, [doc, form])
  const selected = model ? selectedItem(model, at) : undefined
  const group = model?.groups.find(group => `/${group.id}` === at)
  const select = (pointer: string) => {setAt(pointer); setOpen(true)}
  return <EditingContext.Provider value={{
    editing: true, buffer: read, pending, hold,
    write: edit => onChange(edit(read).text),
    diagnosticsAt: () => [], ids: declaredIds(doc ?? {})
  }}><div className={styles.workspace}>
    <Tabs label={msg('Build your pack')} variant="page" scrollable keepMounted fillPanel={form ? tab : 'source'} value={form ? tab : 'source'} onValueChange={setTab} tabs={[
      {value: 'build', label: msg('Build'), panel: model && <BuilderSplit preference="pack" title={selected?.item.label ?? group?.label ?? msg('Editor')} open={open} onClose={() => setOpen(false)} editor={tab === 'build' && at && (selected || group) && <SelectedLogicEditor model={model} pointer={at} wide/>}>
        <PackLogic model={model} at={at} groupId={null} select={select} inspect={select} mode={logic.preferred} onMode={logic.setPreferred}
          query={logic.query} onQuery={logic.setQuery} display={logic.display} onDisplay={logic.setDisplay} viewport={logic.viewport} onViewport={logic.setViewport} nodePositions={logic.nodePositions} onNodePositionsChange={logic.setNodePositions} listScroll={logic.listScroll}
          tools={<PackBuildActions onSelect={select} editorOpen={open} onToggleEditor={() => setOpen(!open)}/>}/>
      </BuilderSplit>},
      {value: 'settings', label: msg('Settings'), panel: form && tab === 'settings' && <div className={styles.scroll}><PackDocumentView document={doc as unknown as PackDocument} active={null}/></div>},
      {value: 'source', label: msg('Source'), panel: <div className={styles.scroll}>
        {!form && <p role="status"><Message text="The draft needs JSON editing before it can be shown as a form. <0/>" slots={[read.index.parseError]}/></p>}
        <label htmlFor="create-draft-json">{msg('Draft document')}</label><CodeArea id="create-draft-json" value={text} onChange={event => onChange(event.target.value)}/>
      </div>}
    ]}/>
  </div></EditingContext.Provider>
}
