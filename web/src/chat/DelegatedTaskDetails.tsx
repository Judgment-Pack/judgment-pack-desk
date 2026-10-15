import {FailureDetails} from '../assistant/FailureDetails'
import type { DelegatedTask } from '../assistant/delegationContract'
import { msg, useLocale } from '../i18n'
import { ReadingDetails } from './ReadingDetails'
import { MessageRenderer } from './MessageRenderer'
import { TOOL_LABELS } from './toolLabels'
import styles from './DelegatedTaskDetails.module.css'
export function DelegatedTaskDetails({task}:{task:DelegatedTask}) {
  useLocale()
  const profile=task.profile==='analyze'?msg('Analysis'):task.profile==='review'?msg('Review'):task.profile==='research'?msg('Research'):task.profile
  const status=task.state==='queued'?msg('Queued'):task.state==='running'?msg('Working…'):task.state==='complete'?msg('Done'):task.state==='failed'?msg('Failed'):msg('Interrupted')
  return <ReadingDetails title={msg('Delegated task')}>
    <div className={styles.details}>
      <p className={styles.title}>{task.title}</p>
      <p className={styles.muted}>{status} · {profile}</p>
      <p>{task.objective}</p>
      {task.problem&&<p role="status" className={styles.problem}>{task.problem}</p>}
      <FailureDetails failure={task.failure}/>
      {task.result&&<MessageRenderer text={task.result}/>}
      {!!task.steps.length&&<ol className={styles.steps}>{task.steps.map((step,i)=><li key={i}><span>{TOOL_LABELS[step.name]??step.name}</span><span className={styles.muted}>{step.state==='complete'?msg('Done'):step.state==='failed'?msg('Failed'):step.state==='running'?msg('Working…'):msg('Interrupted')}</span></li>)}</ol>}
      <p className={styles.muted}>{msg('Generated findings for the parent assistant. No approval or verification is implied.')}</p>
    </div>
  </ReadingDetails>
}
