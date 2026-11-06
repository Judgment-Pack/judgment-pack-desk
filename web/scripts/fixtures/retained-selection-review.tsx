// Offline direct mount only. This file is never imported by a production entry.
import {useRef, useState} from 'react'
import {createRoot} from 'react-dom/client'
import {Tooltip} from 'radix-ui'
import {DIALECT_RETAINED_COLUMN_DRAFT} from '@judgment-pack/harness/dialects'
import {RetainedRecordReview} from '../../src/dialects/RetainedRecordReview'
import type {RetainedSelectionSource} from '../../src/dialects/retainedSelection/wire'
import '../../src/styles.css'
const source: RetainedSelectionSource = {
 preparationId: 'prep_' + '1'.repeat(32), revision: '2'.repeat(64), packDigest: 'sha256:' + 'a'.repeat(64),
 conversionContract: DIALECT_RETAINED_COLUMN_DRAFT, origin: 'asserted', recordIds: ['row-a', 'row-b'],
 requestBytes: new TextEncoder().encode(JSON.stringify({recordIds: ['row-a', 'row-b']})).length,
}
// Only synthetic public source metadata; no session injection or transport mock.
Object.assign(window, {retainedBrowserFixture: {source}})
function Fixture() {
 const opener = useRef<HTMLButtonElement>(null), [open, setOpen] = useState(false)
 return <Tooltip.Provider><main><h1>Inactive retained review offline fixture</h1>
  <button ref={opener} id="open-review" onClick={() => setOpen(true)}>Open fixture review</button>
  {open && <RetainedRecordReview source={source} currentRevision={source.revision} opener={opener} onClose={() => setOpen(false)}/>}
 </main></Tooltip.Provider>
}
createRoot(document.getElementById('root')!).render(<Fixture/>)
