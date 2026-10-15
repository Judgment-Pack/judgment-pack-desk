import {msg, useLocale, systemMessage} from '../i18n'
import {CodeDiff} from '../ui/CodeDiff'
import type {DiffEntry, ProposalDiff} from './proposalDiff'
import styles from './AssistantPane.module.css'
/** Structural summary preserves rule moves; the shared viewer highlights actual lines. */
export function ProposalDiffView({diff,onBaseline=true}:{diff:ProposalDiff;onBaseline?:boolean}) {
  useLocale()
  const changed=diff.entries.filter(e=>e.status!=='unchanged')
  const moves=(entries:DiffEntry[]):DiffEntry[]=>entries.flatMap(e=>[...(e.moved?[e]:[]),...moves(e.children??[])])
  return <section className={styles.diff} aria-label={msg('The proposal as a diff')}>
    <p className={styles.honesty}>{diff.against==='nothing' ? msg('No comparison available: {{reason}}',{reason:systemMessage(diff.reason??'')})
      : onBaseline ? msg('Compared with the document this request started from.') : msg('Compared with the request-time document. The current document has changed.')}</p>
    <p className={styles.honesty}>{changed.length ? msg('Changed: {{members}}',{members:changed.map(e=>e.label).join(', ')}) : msg('No changes.')}</p>
    {moves(diff.entries).map(e=><p key={e.key} className={styles.honesty}>{msg('Moved: {{member}}',{member:e.label})}</p>)}
    {diff.afterText!==undefined && <CodeDiff before={diff.beforeText??''} after={diff.afterText}/>}
  </section>
}
