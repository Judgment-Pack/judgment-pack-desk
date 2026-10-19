import {FailureDetails} from '../assistant/FailureDetails'
import type { DelegatedTask, TaskReport } from '@judgment-pack/harness/delegationContract'
import { msg, useLocale } from '../i18n'
import { ReadingDetails } from './ReadingDetails'
import { MessageRenderer } from './MessageRenderer'
import { TOOL_LABELS } from './toolLabels'
import styles from './DelegatedTaskDetails.module.css'

/** How a profile is named, and the closed set of report kinds the harness writes; an unknown profile is shown as it is spelled. */
export function profileLabel(profile: string): string {
  return profile === 'analyze' ? msg('Analysis') : profile === 'review' ? msg('Review') : profile === 'research' ? msg('Research') : profile === 'author-test' ? msg('Pack check') : profile
}

const seconds = (ms: number) => (ms / 1000).toFixed(1)

/**
 * The marks a finished task may carry, as the harness records them: it reached
 * its time limit and returned what it had written (`partial`), it returned the
 * result of an identical earlier task of the chat with no request (`reused`),
 * or its one refused request was sent again before the first byte (`retried`).
 */
export function taskMarks(task: DelegatedTask): string[] {
  return [
    ...(task.partial ? [msg('Partial: stopped at its time limit')] : []),
    ...(task.reused ? [msg('Reused: the result of an identical earlier task')] : []),
    ...(task.retried ? [msg('Retried once after {{seconds}} s', { seconds: seconds(task.retried.waitMs) })] : [])
  ]
}

const KIND = { question: () => msg('Question'), proposal: () => msg('Proposal'), delegation: () => msg('More delegation') } as const
const SEVERITY = { gap: () => msg('Gap'), inconsistency: () => msg('Inconsistency'), unsupported: () => msg('Unsupported') } as const
const CHECK_STATUS = { passed: () => msg('Passed'), failed: () => msg('Failed'), refused: () => msg('Refused'), error: () => msg('Error') } as const
const CONFIDENCE = { low: () => msg('Low'), medium: () => msg('Medium'), high: () => msg('High'), unstated: () => msg('Not stated') } as const

/**
 * A child's structured report, in the shape its profile writes (`summary`,
 * `findings`, `citations`, `openQuestions`, `confidence`, `notDone`). It is the
 * child's own account and **advice, never approval or verification**: every
 * member is shown as text, a finding with the members its profile gives it (a
 * review's severity, an author-test's check, status, case and fix, a research
 * finding's citations), and an open question is data for the parent and the
 * person, not a failure of the task.
 */
export function TaskReportView({ report }: { report: TaskReport }) {
  useLocale()
  const ids = report.citations.map((citation) => citation.id)
  return (
    <section className={styles.report} aria-label={msg('Report')}>
      <p>{report.summary}</p>
      {report.findings.length > 0 && (
        <>
          <p className={styles.heading}>{msg('Findings')}</p>
          <ol className={styles.findings}>
            {report.findings.map((finding, at) => (
              <li key={at}>
                <span className={styles.tags}>
                  {finding.severity !== undefined && <span className={styles.tag}>{SEVERITY[finding.severity]()}</span>}
                  {finding.check !== undefined && <code>{finding.check}</code>}
                  {finding.status !== undefined && <span className={styles.tag}>{CHECK_STATUS[finding.status]()}</span>}
                  {finding.case !== undefined && <span className={styles.tag}>{msg('Case {{id}}', { id: finding.case })}</span>}
                </span>
                <span>{finding.text}</span>
                {finding.fix !== undefined && <span className={styles.muted}>{msg('Smallest fix: {{fix}}', { fix: finding.fix })}</span>}
                {finding.citations !== undefined && finding.citations.length > 0 && (
                  <span className={styles.muted}>{msg('Cites: {{sources}}', { sources: finding.citations.map((id) => `[${ids.indexOf(id) + 1}]`).join(' ') })}</span>
                )}
              </li>
            ))}
          </ol>
        </>
      )}
      {report.citations.length > 0 && (
        <>
          <p className={styles.heading}>{msg('Sources')}</p>
          <ol className={styles.findings}>
            {report.citations.map((citation) => (
              <li key={citation.id}>
                <code>{citation.id}</code>
                {citation.quote !== undefined && <span className={styles.muted}>{citation.quote}</span>}
              </li>
            ))}
          </ol>
        </>
      )}
      {report.openQuestions.length > 0 && (
        <>
          <p className={styles.heading}>{msg('Open questions')}</p>
          <ul className={styles.findings}>
            {report.openQuestions.map((question, at) => (
              <li key={at}>
                <span className={styles.tags}><span className={styles.tag}>{KIND[question.kind]()}</span></span>
                <span>{question.text}</span>
                {question.ref !== undefined && <span className={styles.muted}>{msg('Kept at {{id}}', { id: question.ref })}</span>}
              </li>
            ))}
          </ul>
        </>
      )}
      <p className={styles.muted}>{msg('Confidence: {{level}}', { level: CONFIDENCE[report.confidence]() })}</p>
      {report.notDone.length > 0 && (
        <>
          <p className={styles.heading}>{msg('Not done')}</p>
          <ul className={styles.findings}>{report.notDone.map((item, at) => <li key={at}><span>{item}</span></li>)}</ul>
        </>
      )}
    </section>
  )
}

/**
 * One delegated task: what it was asked, what it ran under (its profile's
 * budget, the model, the references it was given and the task it continues),
 * what it cost, and what it found, in the profile's report shape. A task that
 * is still running shows the prose it has written so far, which the harness
 * streams under the task's id.
 */
export function DelegatedTaskDetails({task, progress}:{task:DelegatedTask;progress?:string}) {
  useLocale()
  const status=task.state==='queued'?msg('Queued'):task.state==='running'?msg('Working…'):task.state==='complete'?msg('Done'):task.state==='failed'?msg('Failed'):msg('Interrupted')
  const marks=taskMarks(task)
  const tokens=task.tokens
  return <ReadingDetails title={msg('Delegated task')}>
    <div className={styles.details}>
      <p className={styles.title}>{task.title}</p>
      <p className={styles.muted}>{status} · {profileLabel(task.profile)}</p>
      {marks.length>0&&<ul className={styles.marks}>{marks.map(mark=><li key={mark}>{mark}</li>)}</ul>}
      <p>{task.objective}</p>
      {task.problem&&<p role="status" className={styles.problem}>{task.problem}</p>}
      <FailureDetails failure={task.failure}/>
      <dl className={styles.facts}>
        {task.model!==undefined&&<><dt>{msg('Model')}</dt><dd>{task.model}</dd></>}
        {task.budget!==undefined&&<><dt>{msg('Budget')}</dt><dd>{msg('Requests {{requests}} · tool calls {{tools}} · {{seconds}} s',{requests:task.budget.modelRequests,tools:task.budget.toolCalls,seconds:Math.round(task.budget.milliseconds/1000)})}</dd></>}
        {(task.state==='complete'||task.state==='failed'||task.modelRequests>0)&&<><dt>{msg('Used')}</dt><dd>{msg('Requests {{requests}} · tool calls {{tools}}',{requests:task.modelRequests,tools:task.toolCalls})}</dd></>}
        {tokens!==undefined&&(tokens.inputTokens!==undefined||tokens.outputTokens!==undefined)&&<><dt>{msg('Tokens')}</dt><dd>{msg('in {{input}}, out {{output}}',{input:tokens.inputTokens??0,output:tokens.outputTokens??0})}</dd></>}
        {task.firstByteMs!==undefined&&<><dt>{msg('First answer')}</dt><dd>{msg('{{seconds}} s',{seconds:seconds(task.firstByteMs)})}</dd></>}
        {task.durationMs!==undefined&&<><dt>{msg('Time')}</dt><dd>{msg('{{seconds}} s',{seconds:seconds(task.durationMs)})}</dd></>}
        {task.references!==undefined&&task.references.length>0&&<><dt>{msg('References')}</dt><dd className={styles.ids}>{task.references.map(id=><code key={id}>{id}</code>)}</dd></>}
        {task.resumes!==undefined&&<><dt>{msg('Continues task')}</dt><dd><code>{task.resumes}</code></dd></>}
        {task.reused!==undefined&&<><dt>{msg('Result of task')}</dt><dd><code>{task.reused}</code></dd></>}
      </dl>
      {progress&&<section className={styles.progress} aria-label={msg('Written so far')}><MessageRenderer text={progress}/></section>}
      {task.report?<TaskReportView report={task.report}/>:task.result&&<MessageRenderer text={task.result}/>}
      {task.more!==undefined&&<p className={styles.muted}>{task.summarized?msg('This is a summary of {{total}} characters of findings, kept in this chat\'s context as {{id}}; read the rest from {{cursor}}.',{total:task.more.totalCharacters,id:task.more.id,cursor:task.more.cursor}):msg('This is the start of {{total}} characters of findings, kept in this chat\'s context as {{id}}; read the rest from {{cursor}}.',{total:task.more.totalCharacters,id:task.more.id,cursor:task.more.cursor})}</p>}
      {!!task.steps.length&&<ol className={styles.steps}>{task.steps.map((step,i)=><li key={i}><span>{TOOL_LABELS[step.name]??step.name}</span><span className={styles.muted}>{step.state==='complete'?msg('Done'):step.state==='failed'?msg('Failed'):step.state==='running'?msg('Working…'):msg('Interrupted')}</span></li>)}</ol>}
      <p className={styles.muted}>{msg('Generated findings for the parent assistant. No approval or verification is implied.')}</p>
    </div>
  </ReadingDetails>
}
