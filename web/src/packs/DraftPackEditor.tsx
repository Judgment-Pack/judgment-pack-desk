import { useMemo, useState } from 'react'
import type { PackDocument } from '../mcp/types'
import { CodeArea } from '../ui/CodeArea'
import { Tabs } from '../ui/Tabs'
import { agreesWithParse } from './documentText'
import { PackDocumentView } from './document/PackDocumentView'
import { isRecord } from './document/MisshapenMember'
import { EditingContext, declaredIds, type PendingText } from './edit/editingContext'
import { buffered } from './edit/writes'
import { PACK_GROUPS } from './PackWorkspace'

/** Forms and JSON edit the same bytes; held operands survive changing groups. */
export function DraftPackEditor({ text, onChange, pending, hold }: {
  text: string; onChange: (text: string) => void
  pending: ReadonlyMap<string, PendingText>
  hold: (pointer: string, draft: PendingText | null) => void
}) {
  const [tab, setTab] = useState('rules')
  const read = useMemo(() => buffered(text), [text])
  const doc = read.index.value
  const form = isRecord(doc) && agreesWithParse(text, read.index).length === 0
  return <EditingContext.Provider value={{
    editing: true, buffer: read, pending, hold,
    write: (edit) => onChange(edit(read).text),
    diagnosticsAt: () => [], ids: declaredIds(doc ?? {})
  }}>
    <Tabs label="Build your pack" value={form ? tab : 'json'} onValueChange={setTab} tabs={[
      ...(form ? [
        { value: 'rules', label: 'Rules & outcomes', panel: <PackDocumentView key="rules" document={doc as unknown as PackDocument} active={null} members={PACK_GROUPS.rules} outline={false} /> },
        { value: 'evidence', label: 'Evidence & sources', panel: <PackDocumentView key="evidence" document={doc as unknown as PackDocument} active={null} members={PACK_GROUPS.evidence} outline={false} /> },
        { value: 'document', label: 'Full document', panel: <PackDocumentView key="document" document={doc as unknown as PackDocument} active={null} /> }
      ] : []),
      { value: 'json', label: 'JSON', panel: <>
        {!form && <p role="status">The draft needs JSON editing before it can be shown as a form. {read.index.parseError}</p>}
        <label htmlFor="create-draft-json">Draft document</label>
        <CodeArea id="create-draft-json" value={text} onChange={(event) => onChange(event.target.value)} />
      </> }
    ]} />
  </EditingContext.Provider>
}
