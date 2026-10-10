import { msg } from '../i18n'
import type { WorkItem } from '../chat/responseHistory'
import { TOOL_LABELS } from '../chat/toolLabels'
import { CodeBlock } from '../ui/CodeBlock'
import styles from './GraphWorkspace.module.css'
export function GraphStepDetails({row}: {row: WorkItem}) {
  return <section className={styles.inspector}>
    <h2>{TOOL_LABELS[row.name] ?? row.name}</h2>
    {row.name === 'graph_rehearse' && <p>{msg('Rehearsal · No decision audit record or external action. This answer belongs to this tool call.')}</p>}
    {row.graph?.truncated && <p>{msg('This inspection contains a shortened preview. Open the retained draft for its full source.')}</p>}
    {row.graph && <CodeBlock label={msg('Tool arguments')} text={row.graph.arguments}/>}
    {row.graph?.result !== undefined ? <CodeBlock label={msg('Tool result')} text={row.graph.result} lang="en"/> : <p>{msg('Waiting for the tool result…')}</p>}
  </section>
}
