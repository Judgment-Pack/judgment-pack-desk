import { msg, useLocale } from '../i18n'
/**
 * Research and draft: the authoring conversation that researches sources
 * through the gateway, drafts a pack with traceable citations, establishes
 * test cases from the sources, checks the draft through the runtime, and
 * presents the result for review before anything is created.
 *
 * Three regions, as the shell provides them. The main area is the
 * conversation beside the draft review (Draft, Sources, Tests, Review). The
 * Inspector shows whichever source, excerpt or rule was chosen, and nothing
 * the main area already shows. The Console's Activity tab carries what the
 * tools actually did — every search, read, receipt and check as a milestone —
 * and nothing narrated. Creating hands the reviewed draft to the Create page,
 * which names it, validates the bytes and writes them exactly as it always has.
 */
import { useEffect, useState } from 'react'
import { useBlocker, useNavigate } from 'react-router-dom'
import { Conversation, statusLine } from '../research/ui/Conversation'
import { DraftTabs, type Selection } from '../research/ui/DraftPanels'
import { SourceInspector } from '../research/ui/SourceInspector'
import styles from '../research/ui/ResearchAuthoring.module.css'
import { canCreateResearchDraft, matrixDocument, researchRecord } from '../research/run'
import { useResearchRun } from '../research/useResearchRun'
import { useInspectorPortal, useInspectorSlot } from '../shell/InspectorSlot'
import { useMediaQuery } from '../shell/useMediaQuery'
import { Button } from '../ui/Button'
import { Field } from '../ui/Field'
import { PageBody, PageHeader } from '../ui/PageLayout'
import { SegmentedControl } from '../ui/SegmentedControl'
import { TextArea } from '../ui/TextArea'

export const RESEARCH_ROUTE = '/create-pack/research'

/** What the Create page receives from a reviewed run. */
export interface ResearchHandover {
  document: unknown
  name: string
  description: string
  unknowns: string[]
  matrix: unknown
  research: unknown
}

function urlsOf(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => /^https?:\/\//.test(line))
}

export function ResearchAuthoringPage() {
  useLocale()
  const { run, state, ledger, sources, blocked, model } = useResearchRun()
  const [brief, setBrief] = useState('')
  const [urls, setUrls] = useState('')
  const [selection, setSelection] = useState<Selection>(null)
  const [shown, setShown] = useState<'conversation' | 'draft'>('conversation')
  const narrow = useMediaQuery('(max-width: 1100px)')
  const navigate = useNavigate()
  const slot = useInspectorSlot()
  const running = state.status === 'running'

  // Leaving the page ends the run: nothing about it is persisted, and a run
  // nobody is watching is a run spending budget for nobody.
  useEffect(() => () => run?.stop(), [run])
  const blocker = useBlocker(({ currentLocation, nextLocation }) => running && currentLocation.pathname !== nextLocation.pathname)
  useEffect(() => {
    if (blocker.state !== 'blocked') return
    if (window.confirm(msg('Leave and stop the research run? The draft so far will be discarded.'))) blocker.proceed()
    else blocker.reset()
  }, [blocker])

  const select = (next: Selection) => {
    setSelection(next)
    if (next !== null) slot.reveal()
  }
  const inspector = useInspectorPortal(ledger === null ? null : <SourceInspector selection={selection} ledger={ledger} state={state} />)

  const latest = state.candidates.at(-1)
  const passing = canCreateResearchDraft(state)
  const create = () => {
    if (!latest || !passing || ledger === null || running) return
    const document = latest.document as { title?: unknown; decision?: { question?: unknown } }
    const handover: ResearchHandover = {
      document: latest.document,
      name: typeof document.title === 'string' ? document.title : '',
      description: typeof document.decision?.question === 'string' ? document.decision.question : '',
      unknowns: state.unknowns,
      matrix: matrixDocument(state, ledger),
      research: researchRecord(state, ledger, '')
    }
    navigate('/create-pack', { state: { research: handover } })
  }

  const start = () => {
    if (run === null || blocked !== '' || brief.trim() === '') return
    setShown('conversation')
    run.start(brief.trim(), urlsOf(urls))
  }

  return (
    <div data-layout="page">
      <PageHeader
        title={msg("Packs")}
        context={msg("Research and draft")}
        meta={state.phase === 'idle' ? (model ? msg("model {{value0}}", { value0: model }) : undefined) : statusLine(state)}
        actions={
          state.phase === 'idle' ? undefined : running ? (
            <Button onClick={() => run?.stop()}>{msg("Stop")}</Button>
          ) : (
            <Button variant="primary" disabled={!passing} onClick={create}>{msg("Create pack")}</Button>
          )
        }
      />
      {inspector}
      <PageBody width="wide">
        {state.phase === 'idle' ? (
          <form
            className={styles.intro}
            noValidate
            onSubmit={(event) => {
              event.preventDefault()
              start()
            }}
          >
            <h1>{msg("What decision should this pack help make?")}</h1>
            <p>{msg("Describe one decision and its scope. The assistant researches official sources through the configured gateway, drafts the pack with every requirement cited to an excerpt it read, has a reviewer establish test cases from those excerpts, checks the draft through the runtime, and presents it here for your review. Nothing is written until you create the pack.")}</p>
            {blocked !== '' && (
              <p className={styles.detail} role="status">
                {blocked}
              </p>
            )}
            <Field label={msg("The decision (required)")} hint={msg("For example: screen applicants against the Federal Skilled Worker Program's published minimum requirements, before any invitation, admissibility or final approval.")}>
              {(wiring) => <TextArea {...wiring} rows={5} value={brief} onChange={(event) => setBrief(event.target.value)} />}
            </Field>
            <Field label={msg("Read these first (optional)")} hint={msg("One URL per line. Official pages and PDFs are read before any search.")}>
              {(wiring) => <TextArea {...wiring} rows={3} value={urls} onChange={(event) => setUrls(event.target.value)} />}
            </Field>
            <div>
              <Button variant="primary" type="submit" disabled={blocked !== '' || brief.trim() === ''}>{msg("Start research")}</Button>
            </div>
          </form>
        ) : (
          <>
            {narrow && (
              <div className={styles.switch}>
                <SegmentedControl
                  label={msg("Workspace view")}
                  value={shown}
                  onValueChange={(next) => setShown(next as 'conversation' | 'draft')}
                  segments={[
                    { value: 'conversation', label: msg("Conversation") },
                    { value: 'draft', label: msg("Draft") }
                  ]}
                />
              </div>
            )}
            <div className={styles.workspace} data-narrow={narrow}>
              <div data-pane="conversation" data-shown={!narrow || shown === 'conversation'} className={styles.pane} style={{ display: 'contents' }}>
                <Conversation state={state} onSend={(text) => run?.send(text)} onStop={() => run?.stop()} onRetryValidation={() => run?.retryExpectationValidation()} />
              </div>
              <div data-pane="draft" data-shown={!narrow || shown === 'draft'} style={{ display: 'contents' }}>
                <DraftTabs onProposeCorrection={id => run?.proposeExpectationCorrection(id)} onApproveCorrection={(id, token) => run?.approveExpectationCorrection(id, token)} state={state} sources={sources} selection={selection} onSelect={select} onCreate={create} />
              </div>
            </div>
          </>
        )}
      </PageBody>
    </div>
  )
}
