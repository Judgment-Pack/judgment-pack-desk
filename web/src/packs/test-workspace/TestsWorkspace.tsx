import { DropdownMenu } from 'radix-ui'
import { useEffect, useMemo, useRef, useState } from 'react'
import { msg, useLocale } from '../../i18n'
import { useMcp } from '../../mcp/McpProvider'
import type { PackTest, PackDocument } from '../../mcp/types'
import type { McpToolResult } from '../../assistant/engine'
import { useChats } from '../../chat/ChatProvider'
import type { ChatAttachment } from '../../chat/store'
import {
  useChatAttachments,
  TEXT_ATTACHMENT_ACCEPT,
  type AttachmentDestination,
} from '../../chat/useChatAttachments'
import { useConnectionsPane } from '../../connections/ConnectionPaneContext'
import { useEffectiveConfig } from '../../config/DeskConfigProvider'
import { useFileListing } from '../../files/queries'
import { readFile } from '../../files/client'
import { digestOf, jsonIdentity } from '../../research/checkCandidate'
import { validateExpectations } from '../../research/expectations'
import { useInspectorPresentation } from '../../shell/InspectorPresentation'
import { useInspectorPortal, useInspectorControls } from '../../shell/InspectorSlot'
import { useBriefSubject } from '../../briefs/context'
import { caseSnapshot } from '../../briefs/model'
import { useDetailsPortal, useDetailsSlot } from '../../shell/DetailsSlot'
import { useShellState } from '../../shell/paneState'
import { useDirtyGuard } from '../../shell/useDirtyGuard'
import { Button } from '../../ui/Button'
import { Tooltip, OverflowTooltip } from '../../ui/Tooltip'
import { Dialog, DialogActions } from '../../ui/Dialog'
import { IconCopy, IconTrash, IconMore } from '../../shell/icons'
import { Input } from '../../ui/Input'
import { SegmentedControl } from '../../ui/SegmentedControl'
import { InspectionRow } from '../../ui/InspectionRow'
import { Select } from '../../ui/Select'
import { Disclosure } from '../../ui/Disclosure'
import { CodeBlock } from '../../ui/CodeBlock'
import { valueLabel } from '../terminology'
import { MatrixRowList } from '../../components/MatrixRowList'
import { CoverageReport } from '../../components/CoverageReport'
import { CaseEditor } from './CaseEditor'
import { TestAssistant } from './TestAssistant'
import { currentCoverage, latestCoverage, missingProbes } from './coverage'
import { ProposalReview } from './ProposalReview'
import { RunComparison } from './RunComparison'
import { proposalCases, saveReviewedCases, validateTestProposal, type TestProposal } from './proposals'
import { useTestStorage, updateTests } from './store'
import {
  executable,
  expectedLabel,
  importMatrix,
  latestResult,
  matrix,
  newCase,
  object,
  recoverDraft,
  recoverResearchRecord,
  type TestCase,
  type TestRun,
} from './model'
import type { PackDraft } from '../drafts/model'
import styles from './TestsWorkspace.module.css'

function toolPayload(result: McpToolResult): unknown {
  const text = (result.content ?? [])
    .filter((b) => b.type === 'text')
    .map((b) => b.text ?? '')
    .join('\n')
  if (result.isError) throw Error(text || 'The runtime could not complete the test request.')
  return result.structuredContent ?? JSON.parse(text)
}
function trialOutcome(run: TestRun): string {
  const disposition = run.trial?.disposition
  if (!object(disposition)) return msg('Result unavailable')
  if (disposition.kind === 'outcome') {
    try {
      const snapshot = JSON.parse(run.packText)
      const outcomes = Array.isArray(snapshot.outcomes) ? snapshot.outcomes : []
      const outcome = outcomes.find((o: unknown) => object(o) && o.id === disposition.outcomeId)
      return String(outcome?.label ?? disposition.outcomeId ?? msg('Outcome'))
    } catch {
      return String(disposition.outcomeId ?? msg('Outcome'))
    }
  }
  if (disposition.kind === 'unresolved') return msg('Unresolved')
  if (disposition.kind === 'not-applicable') return msg('Not applicable')
  return String(disposition.kind ?? msg('Result unavailable'))
}
export async function carryDraftTests(draft: PackDraft, packId: string) {
  return updateTests(packId, async (s, doc) => {
    const held = doc.suites[draft.id]
    const merged = 'merge:' + draft.id + ':' + draft.generation
    if (held && !s.recovered.includes(merged)) {
      s.recovered.push(merged)
      s.deleted = [...new Set([...(s.deleted ?? []), ...(held.deleted ?? [])])]
      s.cases.push(...held.cases.filter((c) => !s.cases.some((x) => x.id === c.id)))
      s.runs.push(...held.runs.filter((r) => !s.runs.some((x) => x.id === r.id)))
      s.proposals = [
        ...(s.proposals ?? []),
        ...(held.proposals ?? []).filter((p) => !s.proposals?.some((x) => x.id === p.id)),
      ]
      s.messages = held.messages
      s.recovered = [...new Set([...s.recovered, ...held.recovered])]
    }
    return recoverDraft(s, draft)
  })
}
export function TestsContent({
  owner,
  document: doc,
  text,
  title,
  matrixPath,
  active = true,
  draft,
  initialHistory = false,
}: {
  owner: string
  document: PackDocument
  text: string
  title: string
  matrixPath?: string
  active?: boolean
  draft?: PackDraft
  initialHistory?: boolean
}) {
  const locale = useLocale(),
    { client, status, connectionEpoch } = useMcp(),
    storage = useTestStorage(owner),
    { suite } = storage
  const chats = useChats(),
    shell = useShellState(),
    assistant = useInspectorControls(),
    details = useDetailsSlot(),
    connections = useConnectionsPane(),
    config = useEffectiveConfig()
  const [open, setOpen] = useState(false),
    [working, setWorking] = useState<TestCase | null>(null),
    [baseline, setBaseline] = useState(''),
    [runView, setRunView] = useState<TestRun | null>(null),
    [sourceMenu, setSourceMenu] = useState(false)
  const [query, setQuery] = useState(''),
    [filter, setFilter] = useState('all'),
    [history, setHistory] = useState(initialHistory),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [digest, setDigest] = useState(''),
    [supported, setSupported] = useState(false)
  const [reviewing, setReviewing] = useState<TestProposal | null>(null)
  const [reviewRows, setReviewRows] = useState<TestCase[]>([])
  const [reviewEditing, setReviewEditing] = useState(false)
  const digestRef = useRef(digest)
  digestRef.current = digest
  const [pendingDelete, setPendingDelete] = useState<TestCase | null>(null)
  const [deleteError, setDeleteError] = useState('')
  const deleteOpener = useRef<HTMLElement | null>(null)
  const newCaseButton = useRef<HTMLButtonElement | null>(null)
  const [retry, setRetry] = useState(0)
  const [aiSources, setAISources] = useState<ChatAttachment[]>([]),
    [aiRequest, setAIRequest] = useState<{ id: number; text: string; additionsOnly?: boolean; coverageRunId?: string }>({ id: 0, text: '' })
  const [sourceTarget, setSourceTarget] = useState<'case' | 'ai'>('case')
  const workRef = useRef(working),
    sourceRef = useRef(aiSources),
    mounted = useRef(true),
    running = useRef(false)
  workRef.current = working
  sourceRef.current = aiSources
  const briefCase = working && suite.cases.find(c => c.id === working.id)
  useBriefSubject(active ? { id: briefCase ? `case:${owner}:${briefCase.id}` : `case:${owner}:select`, path: '/api/briefs', title, owner, caseId: briefCase?.id,
    ...(briefCase ? { snapshot: caseSnapshot(text, briefCase, suite) } : { unavailable: msg('Select a saved test case to read or generate its brief. Save a new case first.') }) } : null)
  const dirty = !!working && jsonIdentity(working) !== baseline
  const savedProposalCases =
    (suite.proposals?.find((p) => p.id === reviewing?.id) ?? reviewing)?.savedCases ?? {}
  const reviewDirty =
    !!reviewing &&
    reviewRows.some(
      (c) =>
        !Object.hasOwn(savedProposalCases, c.id) &&
        jsonIdentity(c) !== jsonIdentity(proposalCases(reviewing).find((p) => p.id === c.id)),
    )
  useDirtyGuard(active && (dirty || reviewDirty), msg('Leave without saving these test changes?'))
  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])
  useEffect(() => {
    let keep = true
    setDigest('')
    void digestOf(text).then((v) => {
      if (keep) setDigest(v)
    })
    return () => {
      keep = false
    }
  }, [text])
  useEffect(() => {
    let keep = true
    setSupported(false)
    if (client && status === 'ready')
      void client
        .listTools()
        .then((r) => {
          if (keep) setSupported(r.tools.some((t) => t.name === 'experimental_test_cases'))
        })
        .catch(() => {})
    return () => {
      keep = false
    }
  }, [client, status, connectionEpoch])
  const presentation = useMemo(
    () => ({
      title: msg('Assistant'),
      contextTitle: title,
      workspaceTools: true,
      available: true,
      open,
      onOpenChange: setOpen,
      width: shell.inspectorWidth ?? 440,
      onResize: shell.resizeInspector,
      onReset: shell.resetInspectorWidth,
      minimumMainWidth: 480,
      maximumWidth: 640,
    }),
    [locale, title, open, shell.inspectorWidth, shell.resizeInspector, shell.resetInspectorWidth],
  )
  useInspectorPresentation(active ? presentation : null)
  const listing = useFileListing()
  const researchPath = matrixPath?.endsWith('.matrix.json')
    ? matrixPath.replace(/\.matrix\.json$/, '.research.json')
    : undefined
  const researchFile = listing.data?.files.find((f) => f.path === researchPath)
  const attempted = useRef('')
  const artifacts = draft ? [draft] : chats.packDrafts.filter((d) => d.finalized?.id === owner)
  const recoveryKey = JSON.stringify([
    owner,
    artifacts.map((d) => [
      d.id,
      d.generation,
      d.checkpoint.state.probes,
      d.checkpoint.state.cases,
      d.checkpoint.state.candidates.map((c) => [c.digest, c.check, c.previousCheck]),
    ]),
    matrixPath,
    researchFile?.sha256,
    retry,
  ])
  useEffect(() => {
    if (!storage.query.isSuccess || !chats.ready || attempted.current === recoveryKey) return
    attempted.current = recoveryKey
    void storage
      .update(async (s, document) => {
        for (const artifact of artifacts) {
          if (artifact.finalized) {
            const held = document.suites[artifact.id],
              merged = 'merge:' + artifact.id + ':' + artifact.generation
            if (held && !s.recovered.includes(merged)) {
              s.recovered.push(merged)
              s.deleted = [...new Set([...(s.deleted ?? []), ...(held.deleted ?? [])])]
              s.cases.push(...held.cases.filter((c) => !s.cases.some((x) => x.id === c.id)))
              s.runs.push(...held.runs.filter((r) => !s.runs.some((x) => x.id === r.id)))
            }
          }
          s = await recoverDraft(s, artifact)
        }
        if (matrixPath && !s.recovered.includes('matrix:' + matrixPath)) {
          const file = await readFile(matrixPath),
            rows = importMatrix(JSON.parse(file.content))
          s.cases.push(...rows.filter((c) => !s.cases.some((x) => x.id === c.id)))
          s.recovered.push('matrix:' + matrixPath)
        }
        if (researchFile && !s.recovered.includes('research:' + researchFile.sha256)) {
          const record = JSON.parse((await readFile(researchFile.path)).content)
          s = await recoverResearchRecord(s, record, 'research:' + researchFile.sha256)
          s.recovered.push('research:' + researchFile.sha256)
        }
        return s
      })
      .catch((e) => setError((e as Error).message))
  }, [storage.query.isSuccess, chats.ready, recoveryKey])
  function select(c: TestCase, reveal = true) {
    if (dirty && !window.confirm(msg('Discard unsaved changes to this case?'))) return false
    const next = structuredClone(c)
    setWorking(next)
    setBaseline(jsonIdentity(next))
    setSourceMenu(false)
    setRunView(null)
    if (reveal) details.reveal()
    return true
  }
  function startNew() {
    const next = newCase()
    if (select(next)) setBaseline('')
  }
  function requestAI(fill = false) {
    setAISources(working?.sources ?? aiSources)
    setAIRequest({
      id: Date.now(),
      text:
        fill && working
          ? `Propose inputs for this case without changing its intended expectation: ${working.name}. Current case: ${JSON.stringify(working.row)}`
          : 'Design test cases for this pack. Cover normal decisions, exceptions, missing evidence and boundaries.',
    })
    setOpen(true)
    assistant.reveal?.()
  }
  function designMissing(run: TestRun) {
    if (!currentCoverage(run, suite.cases, digest) || !missingProbes(run).length) return
    setAISources([...new Map([...aiSources, ...suite.cases.flatMap(c => c.sources)].map(s => [s.id, s])).values()])
    setAIRequest({ id: Date.now(), text: msg('Design additional test cases for the uncovered behavior in this run. Keep existing cases unchanged and explain any gaps that need a policy decision.'), additionsOnly: true, coverageRunId: run.id })
    setOpen(true)
    assistant.reveal?.()
  }
  function review(proposal: TestProposal) {
    if (busy) return
    if (reviewing?.id === proposal.id) {
      assistant.revealMain?.()
      return
    }
    if ((dirty || reviewDirty) && !window.confirm(msg('Discard unsaved test changes?'))) return
    setWorking(null)
    setBaseline('')
    setReviewEditing(false)
    setError('')
    setReviewRows(proposalCases(proposal))
    setReviewing(proposal)
    assistant.revealMain?.()
  }
  function closeReview() {
    if ((dirty || reviewDirty) && !window.confirm(msg('Discard unsaved test changes?'))) return
    setWorking(null)
    setBaseline('')
    setReviewEditing(false)
    setReviewing(null)
    setHistory(false)
    setError('')
  }
  async function saveSelected(cases: TestCase[]) {
    if (!reviewing || busy || !client) return
    const proposal = reviewing,
      snapshot = structuredClone(cases),
      packDigest = digest
    setBusy(true)
    setError('')
    try {
      const sources = [...new Map(snapshot.flatMap((c) => c.sources).map((s) => [s.id, s])).values()]
      const errors = await validateTestProposal(
        {
          matrixVersion: '3',
          cases: snapshot.map((c) => c.row),
          sourceMappings: Object.fromEntries(snapshot.map((c) => [c.id, c.sourceMappings])),
        },
        sources,
        async (name, args) => (await client.callTool({ name, arguments: args })) as McpToolResult,
        new AbortController().signal,
      )
      if (errors.length) throw Error(errors.map((f) => `${f.path}: ${f.message}`).join('\n'))
      if (!mounted.current || digestRef.current !== packDigest)
        throw Error(msg('The pack changed. Request new suggestions for this revision.'))
      const next = await storage.update((s) => {
        if (!mounted.current || digestRef.current !== packDigest)
          throw Error(msg('The pack changed. Request new suggestions for this revision.'))
        return saveReviewedCases(s, proposal, snapshot, packDigest)
      })
      setReviewing(next.proposals?.find((p) => p.id === proposal.id) ?? proposal)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const destination: AttachmentDestination = useMemo(
    () => ({
      kind: 'test-case',
      id: owner + ':' + sourceTarget + ':' + (working?.id ?? 'ai'),
      current: () => (sourceTarget === 'case' ? workRef.current?.sources : sourceRef.current),
      append: (files) => {
        if (!mounted.current) throw Error(msg('This test workspace is no longer open.'))
        if (sourceTarget === 'case') {
          const w = workRef.current
          if (!w || w.id !== working?.id)
            throw Error(msg('The selected case changed. Choose the source again.'))
          if (w.sources.length + files.length > 4) throw Error(msg('Attach up to four files at a time.'))
          setWorking({ ...w, sources: [...w.sources, ...files] })
          setSourceMenu(false)
          if (reviewEditing) assistant.reveal?.()
        } else {
          if (sourceRef.current.length + files.length > 4)
            throw Error(msg('Attach up to four files at a time.'))
          setAISources([...sourceRef.current, ...files])
          setSourceMenu(false)
          setOpen(true)
          assistant.reveal?.()
        }
      },
    }),
    [owner, working?.id, sourceTarget, reviewEditing],
  )
  const upload = useChatAttachments(null, '', busy, config.config.research, destination)
  const fileInput = useRef<HTMLInputElement>(null),
    importInput = useRef<HTMLInputElement>(null)
  function addSource(target: 'case' | 'ai', _element: HTMLElement) {
    setSourceTarget(target)
    setSourceMenu(true)
    details.reveal()
  }
  async function save() {
    if (!working || busy || !working.name.trim()) return
    const held = structuredClone(working)
    if (reviewEditing) {
      setReviewRows((rows) => rows.map((c) => (c.id === held.id ? held : c)))
      setWorking(null)
      setBaseline('')
      setReviewEditing(false)
      setError('')
      return
    }
    setBusy(true)
    setError('')
    try {
      if (held.row.expectedDisposition !== undefined) {
        if (!client) throw Error(msg('Connect to the runtime before saving an expectation.'))
        const checked = await validateExpectations(
          [held.row.expectedDisposition],
          async (name, args) => (await client.callTool({ name, arguments: args })) as McpToolResult,
          new AbortController().signal,
        )
        if (checked[0]?.status !== 'valid')
          throw Error(checked[0]?.status === 'invalid' ? checked[0].message : 'Expectation was not checked.')
        held.row.expectedDisposition = JSON.parse(checked[0].canonical)
      }
      if (executable(held)) importMatrix({ matrixVersion: '3', cases: [held.row] })
      const next = await storage.update((s) => {
        const current = s.cases.find((c) => c.id === held.id)
        if ((current?.revision ?? 0) !== held.revision)
          throw Error(msg('This case changed in another window. Reload before saving.'))
        if (!current && s.cases.length >= 256) throw Error(msg('This suite has reached its 256-case limit.'))
        held.revision++
        s.deleted = s.deleted?.filter((id) => id !== held.id)
        s.cases = current ? s.cases.map((c) => (c.id === held.id ? held : c)) : [...s.cases, held]
        return s
      })
      const saved = next.cases.find((c) => c.id === held.id)!
      if (workRef.current?.id === held.id) {
        setWorking(saved)
        setBaseline(jsonIdentity(saved))
      }
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  async function runCases(cases: TestCase[]) {
    if (!client || !digest || running.current || !cases.length) return
    running.current = true
    setBusy(true)
    setError('')
    const snapshots = structuredClone(cases),
      entry: TestRun = {
        id: crypto.randomUUID(),
        at: new Date().toISOString(),
        packDigest: digest,
        packText: text,
        packVersion: String(doc.version ?? ''),
        cases: snapshots,
        origin: 'tests',
      }
    try {
      if (cases.length === 1 && !executable(cases[0]!)) {
        const c = cases[0]!,
          result = toolPayload(
            (await client.callTool({
              name: 'experimental_evaluate',
              arguments: {
                pack: text,
                facts: JSON.stringify(c.row.facts),
                ...(c.row.evidenceAvailability === undefined
                  ? {}
                  : { evidence: JSON.stringify(c.row.evidenceAvailability) }),
                rehearsal: true,
              },
            })) as McpToolResult,
          )
        if (!object(result) || result.status !== 'evaluated' || result.rehearsal !== true)
          throw Error(msg('The runtime did not return a completed rehearsal.'))
        entry.evaluation = result
        entry.trial = {
          documentDigest: digest,
          at: entry.at,
          facts: c.row.facts,
          disposition: result.disposition,
          ...(c.row.evidenceAvailability === undefined ? {} : { evidence: c.row.evidenceAvailability }),
          ...(result.handoffTarget === undefined ? {} : { handoffTarget: result.handoffTarget }),
        }
      } else {
        if (!supported) throw Error(msg('Restart Desk with a runtime that supports snapshot test cases.'))
        const eligible = snapshots.filter(executable)
        if (!eligible.length) throw Error(msg('Add an expected result before running the suite.'))
        const result = toolPayload(
          (await client.callTool({
            name: 'experimental_test_cases',
            arguments: { pack: text, matrix: JSON.stringify(matrix(eligible)) },
          })) as McpToolResult,
        )
        if (
          !object(result) ||
          !Array.isArray(result.packs) ||
          !object(result.summary) ||
          result.summary.total !== eligible.length
        )
          throw Error(msg('The runtime returned an incomplete test report.'))
        entry.report = result as unknown as PackTest
      }
    } catch (e) {
      entry.error = (e as Error).message
      setError(entry.error)
    }
    try {
      await storage.update((s) => ({ ...s, runs: [...s.runs, entry] }))
      if (mounted.current) {
        setRunView(entry)
        setSourceMenu(false)
        details.reveal()
      }
    } catch (e) {
      setError(msg('The run finished, but history could not be saved: ') + (e as Error).message)
      setRunView(entry)
    } finally {
      running.current = false
      setBusy(false)
    }
  }
  async function remove() {
    if (!pendingDelete || busy) return
    const selected = pendingDelete
    setBusy(true)
    setDeleteError('')
    try {
      await storage.update((s) => {
        const current = s.cases.find((c) => c.id === selected.id)
        if (current && jsonIdentity(current) !== jsonIdentity(selected))
          throw Error(msg('This case changed in another window. Reload before deleting.'))
        return {
          ...s,
          deleted: [...new Set([...(s.deleted ?? []), selected.id])],
          cases: s.cases.filter((c) => c.id !== selected.id),
        }
      })
      if (workRef.current?.id === selected.id) {
        setWorking(null)
        setBaseline('')
      }
      setPendingDelete(null)
    } catch (e) {
      setDeleteError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }
  function exportCases() {
    const blob = new Blob([JSON.stringify(matrix(suite.cases), null, 2) + '\n'], {
        type: 'application/json',
      }),
      url = URL.createObjectURL(blob),
      a = window.document.createElement('a')
    a.href = url
    a.download = owner + '.matrix.json'
    a.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
  async function importCases(file?: File) {
    if (!file) return
    try {
      if (file.size > 4 * 1024 * 1024) throw Error(msg('Choose a test matrix under 4 MiB.'))
      const rows = importMatrix(JSON.parse(await file.text()))
      await storage.update((s) => {
        if (rows.some((c) => s.cases.some((x) => x.id === c.id)))
          throw Error(msg('Some case IDs already exist. Rename them before importing.'))
        if (s.cases.length + rows.length > 256) throw Error(msg('This suite has reached its 256-case limit.'))
        return { ...s, cases: [...s.cases, ...rows] }
      })
    } catch (e) {
      setError((e as Error).message)
    }
  }
  function openTrial(r: TestRun) {
    if (!r.trial) return
    const c = newCase()
    c.name =
      msg('Draft trial') +
      ' ' +
      (suite.runs.filter((run) => run.trial && run.origin === 'draft').findIndex((run) => run.id === r.id) +
        1)
    c.origin = 'draft'
    c.row.facts = r.trial.facts
    if (r.trial.evidence !== undefined) c.row.evidenceAvailability = r.trial.evidence
    if (select(c)) setBaseline('')
  }
  const caseEditor = working ? (
    <CaseEditor
      key={working.id}
      owner={owner}
      document={doc}
      value={working}
      onChange={setWorking}
      onSave={() => void save()}
      onDiscard={() => {
        setWorking(null)
        setBaseline('')
        setError('')
        setReviewEditing(false)
      }}
      onAddSource={(el) => addSource('case', el)}
      onAI={() => requestAI(true)}
      onRun={() => void runCases([working])}
      busy={busy || upload.reading}
      error={error}
      dirty={dirty}
      proposal={reviewEditing}
    />
  ) : null
  const detailNode = sourceMenu ? (
    <div className={styles.sourceMenu}>
      <h3>{msg('Add source')}</h3>
      <div className={styles.sourceChoices}>
        <InspectionRow
          label={msg('Upload file')}
          onClick={() => fileInput.current?.click()}
          disabled={upload.reading}
        />
        <InspectionRow
          label={msg('Paste link')}
          onClick={(e) => connections.open({ source: 'web', destination, opener: e.currentTarget })}
        />
        <InspectionRow
          label={msg('Connected sources')}
          onClick={(e) => connections.open({ destination, opener: e.currentTarget })}
        />
      </div>
      <p className={styles.muted}>
        {msg('Sources are references. Review their values before mapping them to case inputs.')}
      </p>
      {upload.reading && <p role="status">{upload.progress}</p>}
      {upload.error && <p role="alert">{upload.error}</p>}
      <Button
        variant="inline"
        onClick={() => {
          setSourceMenu(false)
          if (sourceTarget === 'ai') {
            setOpen(true)
            assistant.reveal?.()
          }
        }}
      >
        {msg('Back')}
      </Button>
    </div>
  ) : runView ? (
    <div className={styles.results}>
      {working && (
        <Button variant="inline" onClick={() => setRunView(null)}>
          {msg('Back to case')}
        </Button>
      )}
      <header className={styles.resultHeading}>
        <h3>
          {runView.trial
            ? msg('Draft trial') +
              ' ' +
              (suite.runs
                .filter((r) => r.trial && r.origin === 'draft')
                .findIndex((r) => r.id === runView.id) +
                1)
            : msg('Test result')}
        </h3>
        <p className={styles.muted}>
          {new Date(runView.at).toLocaleString()} · {msg('Version')} {runView.packVersion}
        </p>
      </header>
      {runView.packDigest !== digest && (
        <p role="status" className={styles.resultNotice}>
          {msg('Historical result · the current pack is a different revision.')}
        </p>
      )}
      {runView.error && <p role="alert">{runView.error}</p>}
      {runView.report?.packs?.map((p) => (
        <div key={p.id}>
          <MatrixRowList
            rows={p.rows ?? []}
            names={Object.fromEntries(runView.cases.map((c) => [c.id, c.name]))}
          />
          <Disclosure title={msg('Coverage')}>
            <CoverageReport coverage={p.coverage} />
            {missingProbes(runView).length > 0 && <>
              <Button disabled={busy || !currentCoverage(runView, suite.cases, digest)} onClick={() => designMissing(runView)}>{msg('Design missing tests')}</Button>
              {!currentCoverage(runView, suite.cases, digest) && <p className={styles.muted}>{msg('Run the current suite before designing tests from these gaps.')}</p>}
            </>}
          </Disclosure>
        </div>
      ))}
      {runView.trial && (
        <>
          <section className={styles.observed}>
            <h3>{msg('Observed result')}</h3>
            <strong>{trialOutcome(runView)}</strong>
            {object(runView.trial.disposition) &&
              Array.isArray(runView.trial.disposition.reasons) &&
              runView.trial.disposition.reasons.length > 0 && (
                <ul className={styles.observedReasons}>
                  {runView.trial.disposition.reasons.map((reason, i) => (
                    <li key={i}>{valueLabel('triggers', String(reason))}</li>
                  ))}
                </ul>
              )}
            {object(runView.trial.disposition) &&
              object(runView.trial.disposition.handoff) &&
              runView.trial.disposition.handoff.state === 'requested' && (
                <p className={styles.muted}>{msg('Handoff requested')}</p>
              )}
            <p className={styles.muted}>{msg('Exploratory run · no expected result was compared.')}</p>
            <Button onClick={() => openTrial(runView)}>{msg('Open inputs as a case')}</Button>
          </section>
        </>
      )}
      <Disclosure title={msg('Exact run record')}>
        <CodeBlock label="JSON" text={JSON.stringify(runView, null, 2)} />
      </Disclosure>
    </div>
  ) : working && !reviewEditing ? (
    caseEditor
  ) : (
    <div className={styles.results}>
      <p>{msg('Select a case to view inputs, sources and expected results.')}</p>
    </div>
  )
  const detailPortal = useDetailsPortal(
    <div hidden={!active} style={{ height: '100%' }}>
      {detailNode}
    </div>,
  )
  const assistantPortal = useInspectorPortal(
    <div hidden={!active} style={{ height: '100%' }}>
      <TestAssistant
        key={owner}
        document={text}
        packDigest={digest}
        suite={suite}
        sources={aiSources}
        onSource={(el) => addSource('ai', el)}
        onReview={review}
        reviewing={assistant.mainCovered ? undefined : reviewing?.id}
        reviewDisabled={busy}
        onRemoveSource={(id) => setAISources((files) => files.filter((f) => f.id !== id))}
        request={aiRequest}
        onCheckpoint={async (record, rows) => {
          await storage.update((s) => ({
            ...s,
            proposals: [...(s.proposals ?? []).filter((p) => p.id !== record.id), record],
            messages: rows ? [...(s.messages ?? []), ...rows] : s.messages,
          }))
        }}
      />
    </div>,
  )
  const filtered = suite.cases.filter(
    (c) =>
      (c.name + ' ' + c.rationale).toLowerCase().includes(query.toLowerCase()) &&
      (filter === 'all' || latestResult(suite, c, digest).status === filter),
  )
  const coverageRun = latestCoverage(suite, digest)
  const trials = suite.runs.filter((r) => r.trial && r.origin === 'draft')
  return (
    <div className={styles.root} data-tests-workspace>
      {detailPortal}
      {assistantPortal}
      <Dialog
        open={pendingDelete !== null}
        title={msg('Delete case?')}
        description={msg('The case will be removed from this suite. Its run history will remain.')}
        openerRef={deleteOpener}
        onOpenChange={(open) => {
          if (!open && !busy) {
            setPendingDelete(null)
            setDeleteError('')
          }
        }}
        onCloseAutoFocus={(event) => {
          event.preventDefault()
          if (deleteOpener.current?.isConnected) deleteOpener.current.focus({ preventScroll: true })
          else newCaseButton.current?.focus({ preventScroll: true })
        }}
        footer={
          <DialogActions>
            <Button
              disabled={busy}
              onClick={() => {
                setPendingDelete(null)
                setDeleteError('')
              }}
            >
              {msg('Cancel')}
            </Button>
            <Button variant="danger" disabled={busy} onClick={() => void remove()}>
              {busy ? msg('Deleting…') : msg('Delete case')}
            </Button>
          </DialogActions>
        }
      >
        <p className={styles.deleteCaseName}>{pendingDelete?.name}</p>
        {deleteError && (
          <p role="alert" className={styles.error}>
            {deleteError}
          </p>
        )}
      </Dialog>
      <input
        hidden
        ref={fileInput}
        type="file"
        accept={TEXT_ATTACHMENT_ACCEPT}
        multiple
        onChange={(e) => {
          void upload.attach(e.target.files)
          e.target.value = ''
        }}
      />
      <input
        hidden
        ref={importInput}
        type="file"
        accept=".json"
        onChange={(e) => {
          void importCases(e.target.files?.[0])
          e.target.value = ''
        }}
      />
      {reviewing ? (
        <>
          <div className={styles.proposalList} hidden={reviewEditing}>
            <ProposalReview
              key={reviewing.id}
              cases={reviewRows}
              saved={(suite.proposals?.find((p) => p.id === reviewing.id) ?? reviewing).savedCases ?? {}}
              busy={busy}
              stale={!digest || reviewing.packDigest !== digest || status !== 'ready'}
              error={error}
              onEdit={(c) => {
                if (select(c, false)) {
                  setReviewEditing(true)
                  setError('')
                }
              }}
              onSave={(cases) => void saveSelected(cases)}
              onClose={closeReview}
            />
          </div>
          {reviewEditing && <div className={styles.proposalEditor}>{caseEditor}</div>}
        </>
      ) : (
        <>
          <div className={styles.content}>
            <div className={styles.workspaceBody}>
              <div className={styles.toolbar}>
                <SegmentedControl
                  label={msg('Test views')}
                  value={history ? 'history' : 'cases'}
                  onValueChange={(view) => setHistory(view === 'history')}
                  segments={[
                    { value: 'cases', label: msg('Cases') + ' · ' + suite.cases.length },
                    { value: 'history', label: msg('Run history') },
                  ]}
                />
                <div className={styles.actions}>
                  <Button variant="quiet" ref={newCaseButton} onClick={startNew} disabled={busy}>
                    {msg('New case')}
                  </Button>
                  <Button onClick={() => requestAI()} disabled={busy}>
                    {msg('Design with AI')}
                  </Button>
                  <Button
                    variant="primary"
                    onClick={() => void runCases(suite.cases.filter(executable))}
                    disabled={busy || status !== 'ready' || !supported || !suite.cases.some(executable)}
                  >
                    {running.current ? msg('Running…') : msg('Run tests')}
                  </Button>
                  <DropdownMenu.Root>
                    <Tooltip content={msg('More test actions')}><DropdownMenu.Trigger asChild>
                      <Button variant="quiet" size="icon" aria-label={msg('More test actions')}>
                        <IconMore />
                      </Button>
                    </DropdownMenu.Trigger></Tooltip>
                    <DropdownMenu.Portal>
                      <DropdownMenu.Content className="desk-menu" align="end" sideOffset={4}>
                        <DropdownMenu.Item
                          className="desk-menu-item"
                          disabled={busy}
                          onSelect={() => importInput.current?.click()}
                        >
                          {msg('Import cases')}
                        </DropdownMenu.Item>
                        <DropdownMenu.Item
                          className="desk-menu-item"
                          onSelect={exportCases}
                          disabled={!suite.cases.some(executable)}
                        >
                          {msg('Export cases')}
                        </DropdownMenu.Item>
                      </DropdownMenu.Content>
                    </DropdownMenu.Portal>
                  </DropdownMenu.Root>
                </div>
              </div>
              {coverageRun && missingProbes(coverageRun).length > 0 && <div className={styles.coverageAction}>
                <span>{missingProbes(coverageRun).length===1 ? msg('1 coverage gap in the current suite') : msg('{{count}} coverage gaps in the current suite', { count: missingProbes(coverageRun).length })}</span>
                <Button variant="quiet" disabled={busy} onClick={() => designMissing(coverageRun)}>{msg('Design missing tests')}</Button>
              </div>}
              {(error || storage.query.error) && (
                <p role="alert" className={styles.error}>
                  {error || storage.query.error?.message}
                  <Button
                    variant="quiet"
                    onClick={() => {
                      attempted.current = ''
                      setRetry((n) => n + 1)
                      setError('')
                      void storage.query.refetch()
                    }}
                  >
                    {msg('Retry')}
                  </Button>
                </p>
              )}
              {storage.query.isPending ? (
                <p role="status">{msg('Loading tests…')}</p>
              ) : history ? (
                <div className={styles.history}>
                  {suite.runs.length > 0 && <Disclosure title={msg('Compare runs and pack changes')}><RunComparison runs={suite.runs} currentText={text}/></Disclosure>}
                  {!suite.runs.length && <p>{msg('No runs yet. Run a case to save its result here.')}</p>}
                  {[...suite.runs].reverse().map((r) => (
                    <Button
                      variant="quiet"
                      key={r.id}
                      onClick={() => {
                        setRunView(r)
                        setSourceMenu(false)
                        details.reveal()
                      }}
                    >
                      <span>
                        {new Date(r.at).toLocaleString()} ·{' '}
                        {r.origin === 'draft' ? msg('From draft') : msg('Tests')}
                      </span>
                      <span>
                        {r.error
                          ? msg('Could not run')
                          : r.trial
                            ? msg('Exploratory')
                            : r.packDigest !== digest
                              ? msg('Historical')
                              : r.report?.status}
                      </span>
                    </Button>
                  ))}
                </div>
              ) : (
                <>
                  {suite.cases.length > 0 && (
                    <div className={styles.filters}>
                      <Input
                        aria-label={msg('Search cases')}
                        placeholder={msg('Search cases…')}
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                      />
                      <Select
                        id="test-results-filter"
                        aria-label={msg('Test result filter')}
                        value={filter}
                        onValueChange={setFilter}
                        options={[
                          { value: 'all', label: msg('All results') },
                          { value: 'passed', label: msg('Passed') },
                          { value: 'mismatch', label: msg('Mismatch') },
                          { value: 'exploratory', label: msg('Exploratory') },
                          { value: 'not-run', label: msg('Not run') },
                          { value: 'stale', label: msg('Needs rerun') },
                          { value: 'error', label: msg('Could not run') },
                        ]}
                      />
                    </div>
                  )}
                  {suite.cases.length > 0 && !filtered.length && (
                    <p>{msg('No cases match these filters.')}</p>
                  )}
                  {!suite.cases.length ? (
                    <div className={styles.empty}>
                      <h2>{msg('No saved cases yet')}</h2>
                      <p className={styles.muted}>
                        {msg(
                          'Create a case or ask Assistant to design one. Add an expected result to make it repeatable.',
                        )}
                      </p>
                    </div>
                  ) : (
                    <div className={styles.tableWrap}>
                      <table className={`${styles.table} ${styles.casesTable}`}>
                        <thead>
                          <tr>
                            <th>{msg('Case')}</th>
                            <th>{msg('Expected')}</th>
                            <th>{msg('Last result')}</th>
                            <th>{msg('Origin')}</th>
                            <th aria-label={msg('Actions')} />
                          </tr>
                        </thead>
                        <tbody>
                          {filtered.map((c) => {
                            const result = latestResult(suite, c, digest)
                            return (
                              <tr key={c.id} data-selected={working?.id === c.id}>
                                <td>
                                  <OverflowTooltip content={c.name}>
                                    <Button
                                      variant="inline"
                                      className={styles.savedCaseName}
                                      onClick={() => select(c)}
                                    >
                                      {c.name}
                                    </Button>
                                  </OverflowTooltip>
                                </td>
                                <td>{expectedLabel(c.row)}</td>
                                <td>
                                  <Button
                                    variant="inline"
                                    disabled={!result.run}
                                    onClick={() => {
                                      if (result.run) {
                                        setRunView(result.run)
                                        setSourceMenu(false)
                                        details.reveal()
                                      }
                                    }}
                                  >
                                    <span className={styles.status} data-status={result.status}>
                                      {msg(result.label)}
                                    </span>
                                  </Button>
                                </td>
                                <td>
                                  {c.origin === 'draft'
                                    ? msg('Draft')
                                    : c.origin === 'ai'
                                      ? msg('AI')
                                      : c.origin === 'manual'
                                        ? msg('Manual')
                                        : msg('Imported')}
                                </td>
                                <td>
                                  <div className={styles.rowActions}>
                                    <Tooltip content={msg('Duplicate case')}>
                                      <Button
                                        variant="quiet"
                                        size="icon"
                                        aria-label={msg('Duplicate case')}
                                        disabled={busy}
                                        onClick={() => {
                                          const id = 'case-' + crypto.randomUUID()
                                          if (
                                            select({
                                              ...structuredClone(c),
                                              id,
                                              row: { ...c.row, id },
                                              revision: 0,
                                              name: c.name + ' ' + msg('copy'),
                                            })
                                          )
                                            setBaseline('')
                                        }}
                                      >
                                        <IconCopy />
                                      </Button>
                                    </Tooltip>
                                    <Tooltip content={msg('Delete case')}>
                                      <Button
                                        variant="quiet"
                                        size="icon"
                                        aria-label={msg('Delete case')}
                                        disabled={busy}
                                        onClick={(event) => {
                                          deleteOpener.current = event.currentTarget
                                          setDeleteError('')
                                          setPendingDelete(structuredClone(c))
                                        }}
                                      >
                                        <IconTrash />
                                      </Button>
                                    </Tooltip>
                                  </div>
                                </td>
                              </tr>
                            )
                          })}
                        </tbody>
                      </table>
                    </div>
                  )}
                  {trials.length > 0 && (
                    <section className={styles.trials}>
                      <h2>
                        {msg('From draft')} <span className={styles.muted}>· {trials.length}</span>
                      </h2>
                      <p className={styles.muted}>
                        {msg(
                          'Exploratory runs from the draft, without saved expectations. Open a trial to review its result or reuse its inputs.',
                        )}
                      </p>
                      <div className={styles.trialList}>
                        <div className={styles.trialColumns} aria-hidden="true">
                          <span>{msg('Trial')}</span>
                          <span>{msg('Observed result')}</span>
                        </div>
                        {trials.map((r, i) => (
                          <InspectionRow
                            key={r.id}
                            className={styles.trial}
                            label={msg('Draft trial') + ' ' + (i + 1)}
                            value={trialOutcome(r)}
                            current={runView?.id === r.id && details.open}
                            onClick={() => {
                              setRunView(r)
                              setSourceMenu(false)
                              details.reveal()
                            }}
                          />
                        ))}
                      </div>
                    </section>
                  )}
                </>
              )}
              {status === 'ready' && !supported && (
                <p className={styles.footnote}>
                  {msg('Update the runtime to run snapshot test suites. Cases and history remain available.')}
                </p>
              )}
              <div className={styles.storageHelp}>
                <Disclosure title={msg('About cases and history')}>
                  <p className={styles.muted}>
                    {msg(
                      'Cases and history are saved on this computer. Export a matrix to share saved expectations.',
                    )}
                  </p>
                  <p className={styles.muted}>
                    {msg(
                      'Test runs do not change the pack. Results belong to the exact inputs and revision that ran.',
                    )}
                  </p>
                </Disclosure>
              </div>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
