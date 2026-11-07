// Handwritten offline props exercise the production components and stylesheet.
import { createRoot } from 'react-dom/client'
import { GraphWalkDiagram } from '../../src/components/GraphWalkDiagram'
import { GraphNodeItem } from '../../src/routes/GraphView'
import { deriveWalkLayout, readGraphDocument } from '../../src/mcp/graphDocument'
import type { GraphSuiteEntry } from '../../src/mcp/types'
import '../../src/styles.css'
const entry: GraphSuiteEntry = { id: 'presentation', status: 'passed', summary: { total: 0, passed: 0, mismatched: 0 }, coverage: [{ probe: 'node:branch-0:outcome:clear', status: 'covered' }] }
function shape(count: number) {
  const document = readGraphDocument(JSON.stringify({ formatVersion: '1', id: 'presentation', version: '1', nodes: Object.fromEntries(Array.from({ length: count }, (_, i) => [`branch-${i}`, { pack: 'screening' }])), edges: [], result: `branch-${count - 1}` }))
  if (!document.ok) throw new Error(document.reason)
  const layout = deriveWalkLayout(document.document, [])
  if (!layout.drawn) throw new Error(layout.reason)
  return layout.shape
}
const disposition = '{"kind":"outcome","outcomeId":"clear","reasons":[]}'
createRoot(document.getElementById('root')!).render(<main style={{ width: 600, margin: 24 }}>
  <section id="wide"><GraphWalkDiagram entry={entry} shape={shape(10)} /></section>
  <section id="one"><GraphWalkDiagram entry={entry} shape={shape(1)} /></section>
  <section id="two"><GraphWalkDiagram entry={entry} shape={shape(2)} /></section>
  <section id="fallback"><GraphWalkDiagram entry={entry} /></section>
  <ul id="trace"><GraphNodeItem node={{ node: 'screening', status: 'passed', expected: disposition, actual: disposition, trace: [
    { stage: 'rule', id: 'first-rule', condition: 'true' },
    { stage: 'exception', id: 'middle-exception', condition: 'false' },
    { stage: 'rule', id: 'last-rule', condition: 'unknown' }
  ] }} /></ul>
</main>)
