import {QueryStatusNotice} from '../../shell/QueryStatusNotice'
import {LoadingState} from '../../ui/LoadingState'
import {PageLoading} from '../../ui/PageLoading'
import { DialectTestsDialog } from '../../dialects/DialectTestsDialog'
import { TestsCollection } from './TestsCollection'
import { CollectionLayout, CollectionEmpty } from '../../ui/CollectionLayout'
import { DeskStatusNotice } from '../../shell/DeskNotices'
import { Spinner } from '../../ui/Spinner'
import { intentionalFactTypes } from './factTypes'
import { testConversationTurns } from './conversation'
import { Menu as DropdownMenu } from '../../ui/Menu'
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { flushSync } from 'react-dom'
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
import { validateExpectations, type InvalidFinding } from '../../research/expectations'
import { useInspectorControls } from '../../shell/InspectorSlot'
import { BriefPane } from '../../briefs/BriefPane'
import { caseSnapshot } from '../../briefs/model'
import { BuilderSplit } from '../../builders/BuilderSplit'
import { TypedConfirmation, useConfirmDiscard } from '../../shell/UnsavedChanges'
import { useDirtyGuard } from '../../shell/useDirtyGuard'
import { Button } from '../../ui/Button'
import { Tooltip, OverflowTooltip } from '../../ui/Tooltip'
import { Dialog, DialogActions } from '../../ui/Dialog'
import { IconMore, IconPlus, IconAssistant, IconPaperclip, IconDownload, IconPlay, IconInfo, IconChevronRight, IconHistory } from '../../shell/icons'
import { SegmentedControl } from '../../ui/SegmentedControl'
import { InspectionRow } from '../../ui/InspectionRow'
import { Disclosure } from '../../ui/Disclosure'
import { CodeBlock } from '../../ui/CodeBlock'
import { valueLabel } from '../terminology'
import { MatrixRowList } from '../../components/MatrixRowList'
import { CoverageReport } from '../../components/CoverageReport'
import { CaseEditor } from './CaseEditor'
import { currentCoverage, latestCoverage, missingProbes, missingTestsRequest } from './coverage'
import { ProposalReview } from './ProposalReview'
import { RunComparison } from './RunComparison'
import { proposalCases, saveReviewedCases, validateTestProposal, type TestProposal } from './proposals'
import { useTestStorage, updateTests } from './store'
import {
  executable,
  latestResult,
  decodeStore,
  emptySuite,
  importMatrix,
  matrix,
  newCase,
  object,
  recoverDraft,
  mergeDraftSuite,
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
/**
 * An invalid expectation, worded as a research run words it (`findingSummary`),
 * so a person reads the same kind of finding in both places. A §8.3 defect is
 * the runtime's own sentence. A limit says the runtime did not admit the input.
 * An expectation no pack can produce says that the expectation must change, not
 * the draft. Desk's words come from the catalogue; the runtime's message after
 * them is shown as it came. In English the result is findingSummary's sentence.
 */
function invalidExpectation(finding: InvalidFinding): string {
  if (finding.unreachable)
    return msg('This expectation names a disposition no pack can produce, so the expectation must change, not the draft: {{detail}}', { detail: finding.message })
  return finding.admitted ? finding.message : msg('The runtime did not admit this expectation: {{detail}}', { detail: finding.message })
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
    return recoverDraft(mergeDraftSuite(s, doc.suites[draft.id], draft), draft)
  })
}
export function TestsContent({
  chatId,
  owner,
  document: doc,
  text,
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
  chatId?: string
  active?: boolean
  draft?: PackDraft
  initialHistory?: boolean
}) {
  useLocale()
  const { client, status, connectionEpoch } = useMcp(),
    storage = useTestStorage(owner),
    { suite } = storage
  const chats = useChats(),
    assistant = useInspectorControls(),
    connections = useConnectionsPane(),
    config = useEffectiveConfig()
  const [bottomOpen,setBottomOpen]=useState(false)
  const details={open:bottomOpen,reveal:()=>setBottomOpen(true)}
  const [viewParams, setViewParams] = useSearchParams()
  const location = useLocation(), navigate = useNavigate(), {caseId: routeCaseId} = useParams()
  const requestedCase = draft ? viewParams.get('case') : routeCaseId
  const [inputTests,setInputTests]=useState(false)
  const [showSuiteHelp, setShowSuiteHelp] = useState(false)
  const [sort, setSort] = useState(viewParams.get('testSort') ?? 'saved')
  function listParams() {
    const next = new URLSearchParams(viewParams)
    next.delete('case'); next.delete('testProposal'); next.delete('assistant')
    if (linkedChat) next.set('chat',linkedChat.id)
    return next
  }
  function casesHref() { const next=listParams(); return `${draft ? location.pathname : `/packs/${encodeURIComponent(owner)}/matrix`}${next.size ? '?'+next : ''}` }
  function caseHref(id: string) {
    const next=listParams()
    if (draft) next.set('case',id)
    return `${draft ? location.pathname : `/packs/${encodeURIComponent(owner)}/matrix/cases/${encodeURIComponent(id)}`}${next.size ? '?'+next : ''}`
  }
  function updateListParam(key: string, value: string, fallback: string) {
    setViewParams(previous => {const next=new URLSearchParams(previous); if(value===fallback)next.delete(key);else next.set(key,value);return next},{replace:true})
  }
  // Old designer links now open the same retained conversation.
  useEffect(() => {
    if (viewParams.get('assistant') === 'tests') setViewParams(previous => {
      const next = new URLSearchParams(previous); next.delete('assistant'); return next
    }, {replace: true})
  }, [viewParams, setViewParams])
  const availableChats = [...chats.chats, ...chats.drafts]
  const requestedChat = chatId ?? viewParams.get('chat')
  const linkedChat = (requestedChat ? availableChats.find(chat => chat.id === requestedChat && (chat.pack?.id === owner || chat.draftId === owner)) : undefined)
    ?? availableChats.filter(chat => !chat.archived && (chat.pack?.id === owner || chat.draftId === owner)).sort((a,b)=>b.updatedAt.localeCompare(a.updatedAt))[0]
  const linkedBinding = linkedChat ? chats.bindings.get(linkedChat.id) : undefined
  useEffect(() => {
    if (!active || !chats.ready || !chats.store) return
    if (linkedChat) chats.store.activate(linkedChat.id)
  }, [active, chats.ready, chats.store, linkedChat?.id, owner, draft?.id])
  const migrating = useRef(false)
  useEffect(() => {
    if (!active || !chats.store || !linkedChat || !linkedBinding?.run || linkedBinding.run.running || !suite.messages?.length || migrating.current) return
    migrating.current = true
    const messages = suite.messages, destination = linkedChat.id, store = chats.store
    void (async () => {
      const turns = await testConversationTurns(owner,messages,suite)
      store.appendConversation(destination,turns)
      await store.checkpoint(new AbortController().signal)
      // Remove only the legacy rows whose chat write has been acknowledged.
      await storage.update(current=>({...current,messages:current.messages?.filter(message=>!messages.some(saved=>JSON.stringify(saved)===JSON.stringify(message))),
        proposals:current.proposals?.map(proposal=>({...proposal,chatId:proposal.chatId ?? destination}))}))
    })().catch(error=>setError(error.message)).finally(()=>{migrating.current=false})
  }, [active, chats.store, linkedChat?.id, linkedBinding?.run, linkedBinding?.state.status, suite.messages])
  const [working, setWorking] = useState<TestCase | null>(null),
    [baseline, setBaseline] = useState(''),
    [runView, setRunView] = useState<TestRun | null>(null),
    [sourceMenu, setSourceMenu] = useState(false)
  const [query, setQuery] = useState(viewParams.get('testSearch') ?? ''),
    [filter, setFilter] = useState(viewParams.get('testResult') ?? 'all'),
    [history, setHistory] = useState(initialHistory),
    [error, setError] = useState(''),
    [busy, setBusy] = useState(false),
    [digest, setDigest] = useState(''),
    [supported, setSupported] = useState<boolean | null>(null),
    [capabilityError, setCapabilityError] = useState('')
  useEffect(() => {
    setQuery(viewParams.get('testSearch') ?? '')
    setFilter(viewParams.get('testResult') ?? 'all')
    setSort(viewParams.get('testSort') ?? 'saved')
    setHistory(viewParams.get('history') === '1')
  }, [viewParams])
  function resetList() {
    setViewParams(previous => {const next=new URLSearchParams(previous); for(const key of ['testSearch','testResult','testSort'])next.delete(key); return next}, {replace:true})
  }
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
  const workRef = useRef(working), mounted = useRef(true), running = useRef(false)
  workRef.current = working
  const caseResult = working ? latestResult(suite, working, digest) : undefined
  const briefCase = working && suite.cases.find(c => c.id === working.id)
  const [showCaseBrief, setShowCaseBrief] = useState(false)
  const briefOpener = useRef<HTMLButtonElement>(null)
  const briefSubject = briefCase ? { id: `case:${owner}:${briefCase.id}`, path: '/api/briefs', title: briefCase.name, owner, caseId: briefCase.id, snapshot: caseSnapshot(text, briefCase, suite) } : null
  const [unwritten,setUnwritten]=useState(false)
  const dirty = unwritten || !!working && jsonIdentity(working) !== baseline
  const savedProposalCases =
    (suite.proposals?.find((p) => p.id === reviewing?.id) ?? reviewing)?.savedCases ?? {}
  const reviewDirty =
    !!reviewing &&
    reviewRows.some(
      (c) =>
        !Object.hasOwn(savedProposalCases, c.id) &&
        jsonIdentity(c) !== jsonIdentity(proposalCases(reviewing).find((p) => p.id === c.id)),
    )
  const confirmDiscard = useConfirmDiscard()
  const [deleteTyped, setDeleteTyped] = useState('')
  useEffect(() => { setDeleteTyped('') }, [pendingDelete?.id])
  useDirtyGuard(dirty || reviewDirty, msg('Leave without saving these test changes?'), { name: working?.name, busy, shouldBlock: ({currentLocation,nextLocation}) => currentLocation.pathname !== nextLocation.pathname || new URLSearchParams(currentLocation.search).get('case') !== new URLSearchParams(nextLocation.search).get('case') })
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
    setSupported(null)
    setCapabilityError('')
    if (client && status === 'ready')
      void client
        .listTools()
        .then((r) => {
          if (keep) setSupported(r.tools.some((t) => t.name === 'experimental_test_cases'))
        })
        .catch(error => {if(keep)setCapabilityError(error instanceof Error ? error.message : String(error))})
    return () => {
      keep = false
    }
  }, [client, status, connectionEpoch, retry])
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
          if (artifact.finalized) s = mergeDraftSuite(s, document.suites[artifact.id], artifact)
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
  const loadedCase = useRef<string | null | undefined>(undefined)
  useEffect(() => {
    if (!active || !storage.query.isSuccess || loadedCase.current === requestedCase) return
    if (!requestedCase) {
      loadedCase.current = requestedCase
      setWorking(null); setBaseline(''); setRunView(null); setSourceMenu(false); setBottomOpen(false)
      return
    }
    let selected = suite.cases.find(c => c.id === requestedCase)
    if (!selected && location.state?.testCaseOwner === owner) {
      try {
        const held=decodeStore({version:1,suites:{[owner]:{...emptySuite(),cases:[location.state.testCaseDraft]}}}).suites[owner]!.cases[0]
        if (held?.id === requestedCase && held.revision === 0) selected=held
      } catch { /* An invalid browser-history snapshot never replaces saved cases. */ }
    }
    if (!selected) {
      setWorking(null); setBaseline(''); setRunView(null); setSourceMenu(false)
      return
    }
    loadedCase.current = requestedCase
    const next=structuredClone(selected)
    setWorking(next); setBaseline(next.revision===0 ? '' : jsonIdentity(next)); setRunView(null); setSourceMenu(false); setBottomOpen(false)
  }, [active, requestedCase, storage.query.isSuccess, suite.cases, location.state, owner])
  async function select(c: TestCase, reveal = true) {
    if (reveal) {
      navigate(caseHref(c.id), c.revision === 0 ? {state:{testCaseOwner:owner,testCaseDraft:structuredClone(c)}} : undefined)
      return true
    }
    if (dirty && !await confirmDiscard(msg('Discard unsaved changes to this case?'), { name: working?.name, busy })) return false
    const next = structuredClone(c)
    setWorking(next)
    setBaseline(jsonIdentity(next))
    setSourceMenu(false)
    setRunView(null)
    if (reveal) details.reveal()
    return true
  }
  async function startNew() {
    const next = newCase()
    await select(next)
  }
  function openTestChat(request: string, sources: ChatAttachment[] = []) {
    const store = chats.store
    if (!store || !chats.ready) { setError(msg('Chat history is still loading')); return }
    try {
      const chat = linkedChat ?? store.startChat(draft ? undefined : {id:owner,path:'',digest:''}, undefined, false, draft?.id)
      const attachments = [...new Map([...(chat.attachments ?? []), ...sources].map(source => [source.id,source])).values()]
      if (attachments.length > 4) throw Error(msg('Attach up to four files at a time.'))
      const composer = chat.composer.includes(request) ? chat.composer : [chat.composer.trim(), request].filter(Boolean).join('\n\n')
      store.update(chat.id, { composer, attachments })
      store.activate(chat.id)
      setViewParams(previous => { const next = new URLSearchParams(previous); next.set('chat',chat.id); next.delete('assistant'); return next }, {replace:true})
      assistant.reveal()
    } catch (error) { setError((error as Error).message) }
  }
  function requestAI(fill = false) {
    openTestChat(fill && working
      ? `Propose inputs for this case without changing its intended expectation: ${working.name}. Current case: ${JSON.stringify(working.row)}`
      : 'Design test cases for this pack. Cover normal decisions, exceptions, missing evidence and boundaries.', working?.sources ?? [])
  }
  function designMissing(run: TestRun) {
    if (!currentCoverage(run, suite.cases, digest) || !missingProbes(run).length) return
    openTestChat(missingTestsRequest(run))
  }
  const openedProposal = useRef('')
  const requestedProposal = viewParams.get('testProposal')
  useEffect(() => {
    if (!active || !requestedProposal || openedProposal.current === requestedProposal || !storage.query.isSuccess) return
    const proposal = suite.proposals?.find(proposal => proposal.id === requestedProposal)
    if (!proposal || proposal.state !== 'ready') return
    openedProposal.current = requestedProposal
    void review(proposal)
  }, [active, requestedProposal, suite.proposals, storage.query.isSuccess])
  async function review(proposal: TestProposal) {
    if (busy) return
    if (reviewing?.id === proposal.id) {
      assistant.revealMain?.()
      return
    }
    if ((dirty || reviewDirty) && !await confirmDiscard(msg('Discard unsaved test changes?'), { name: working?.name, busy })) return
    setWorking(null)
    setBaseline('')
    setReviewEditing(false)
    setError('')
    setReviewRows(proposalCases(proposal))
    setReviewing(proposal)
    assistant.revealMain?.()
  }
  async function closeReview() {
    if ((dirty || reviewDirty) && !await confirmDiscard(msg('Discard unsaved test changes?'), { name: working?.name, busy })) return
    setWorking(null)
    setBaseline('')
    setReviewEditing(false)
    setReviewing(null)
    setViewParams(previous => { const next = new URLSearchParams(previous); next.delete('testProposal'); return next }, {replace:true})
    openedProposal.current = ''
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
          intentionalFactTypes: intentionalFactTypes(proposal.attempts.at(-1)?.document).filter(entry => snapshot.some(c => c.id === entry.caseId)),
        },
        sources,
        async (name, args) => (await client.callTool({ name, arguments: args })) as McpToolResult,
        new AbortController().signal,
        text,
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
      id: owner + ':case:' + (working?.id ?? ''),
      current: () => workRef.current?.sources,
      append: (files) => {
        if (!mounted.current) throw Error(msg('This test workspace is no longer open.'))
        const w = workRef.current
        if (!w || w.id !== working?.id) throw Error(msg('The selected case changed. Choose the source again.'))
        if (w.sources.length + files.length > 4) throw Error(msg('Attach up to four files at a time.'))
        setWorking({ ...w, sources: [...w.sources, ...files] })
        setSourceMenu(false)
        if (reviewEditing) assistant.revealMain?.()

      },
    }),
    [owner, working?.id, reviewEditing],
  )
  const upload = useChatAttachments(null, '', busy, config.config.research, destination)
  const fileInput = useRef<HTMLInputElement>(null),
    importInput = useRef<HTMLInputElement>(null)
  function addSource(_element: HTMLElement) {
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
          throw Error(checked[0]?.status === 'invalid' ? invalidExpectation(checked[0]) : 'Expectation was not checked.')
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
  function exportCases(cases = suite.cases) {
    const blob = new Blob([JSON.stringify(matrix(cases), null, 2) + '\n'], {
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
  async function openTrial(r: TestRun) {
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
    await select(c)
  }
  const caseEditor = working ? (
    <CaseEditor
      key={working.id}
      owner={owner}
      document={doc}
      value={working}
      onChange={setWorking}
      onUnwritten={setUnwritten}
      onSave={() => void save()}
      onDiscard={async () => {
        if (dirty && !await confirmDiscard(msg('Discard unsaved changes to this case?'), { name: working?.name, busy })) return
        flushSync(()=>{setWorking(null);setBaseline('');setUnwritten(false);setError('');setReviewEditing(false)})
        if (requestedCase) navigate(casesHref())
      }}
      onAddSource={(el) => addSource(el)}
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
      {upload.reading && <LoadingState>{upload.progress}</LoadingState>}
      {upload.error && <p role="alert">{upload.error}</p>}
      <Button
        variant="inline"
        onClick={() => {
          setSourceMenu(false)

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
        {runView.trial && <h3>{msg('Draft trial')} {suite.runs.filter(r => r.trial && r.origin === 'draft').findIndex(r => r.id === runView.id) + 1}</h3>}
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
            rows={(p.rows ?? []).filter(row=>!requestedCase || row.id===requestedCase)}
            names={Object.fromEntries(runView.cases.map((c) => [c.id, c.name]))}
          />
          {!requestedCase && <Disclosure title={msg('Coverage')}>
            <CoverageReport coverage={p.coverage} />
            {missingProbes(runView).length > 0 && <>
              <Button disabled={busy || !currentCoverage(runView, suite.cases, digest)} onClick={() => designMissing(runView)}><IconAssistant />{msg('Design missing tests')}</Button>
              {!currentCoverage(runView, suite.cases, digest) && <p className={styles.muted}>{msg('Run the current suite before designing tests from these gaps.')}</p>}
            </>}
          </Disclosure>}
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
            <Button onClick={() => openTrial(runView)}>{msg('Open dialects as a case')}</Button>
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
  const coverageRun = latestCoverage(suite, digest)
  const listHeader = (<div className={styles.toolbar}>
                <SegmentedControl
                  label={msg('Test views')}
                  value={history ? 'history' : 'cases'}
                  onValueChange={(view) => {setHistory(view === 'history');updateListParam('history',view === 'history'?'1':'','')}}
                  segments={[
                    { value: 'cases', label: msg('Cases') + ' · ' + suite.cases.length },
                    { value: 'history', label: msg('Run history') },
                  ]}
                />
                <div className={styles.actions}>
                  <DropdownMenu.Root><DropdownMenu.Trigger asChild><Button ref={newCaseButton} disabled={busy}><IconPlus />{msg('Add case')}</Button></DropdownMenu.Trigger>
                    <DropdownMenu.Portal><DropdownMenu.Content align="end">
                      <DropdownMenu.Item icon={<IconPlus />} onSelect={()=>void startNew()}>{msg('New case')}</DropdownMenu.Item>
                      {!draft&&<DropdownMenu.Item icon={<IconPaperclip />} onSelect={()=>setInputTests(true)}>{msg('From Dialects')}</DropdownMenu.Item>}
                      <DropdownMenu.Item icon={<IconAssistant />} onSelect={()=>requestAI()}>{msg('Design with AI')}</DropdownMenu.Item>
                    </DropdownMenu.Content></DropdownMenu.Portal>
                  </DropdownMenu.Root>
                  <Button
                    variant="primary"
                    onClick={() => void runCases(suite.cases.filter(executable))}
                    disabled={busy || status !== 'ready' || !supported || !suite.cases.some(executable)}
                  >
                    {running.current?<Spinner/>:<IconPlay />}{running.current ? msg('Running…') : msg('Run tests')}
                  </Button>
                  <DropdownMenu.Root>
                    <Tooltip content={msg('More test actions')}><DropdownMenu.Trigger asChild>
                      <Button variant="quiet" size="icon" aria-label={msg('More test actions')}>
                        <IconMore />
                      </Button>
                    </DropdownMenu.Trigger></Tooltip>
                    <DropdownMenu.Portal>
                      <DropdownMenu.Content align="end" sideOffset={4}>
                        <DropdownMenu.Item icon={<IconPaperclip />}
                          disabled={busy}
                          onSelect={() => importInput.current?.click()}
                        >
                          {msg('Import cases')}
                        </DropdownMenu.Item>
                        <DropdownMenu.Item icon={<IconDownload />}
                          onSelect={()=>exportCases()}
                          disabled={!suite.cases.some(executable)}
                        >
                          {msg('Export cases')}
                        </DropdownMenu.Item>
                        <DropdownMenu.Separator/><DropdownMenu.Item icon={<IconInfo/>} onSelect={()=>setShowSuiteHelp(true)}>{msg('About cases and history')}</DropdownMenu.Item>
                      </DropdownMenu.Content>
                    </DropdownMenu.Portal>
                  </DropdownMenu.Root>
                </div>
              </div>
  )
  return (
    <div className={styles.root} data-tests-workspace>
      {inputTests&&<DialectTestsDialog packId={owner} packText={text} opener={newCaseButton} onClose={()=>setInputTests(false)} onSaved={()=>{setInputTests(false);void storage.query.refetch()}}/>}
      {active&&<QueryStatusNotice id={'test-storage:'+owner} label={msg('Tests unavailable')} error={storage.query.error} retrying={storage.query.isFetching} onRetry={()=>storage.query.refetch()}/>}
      {active && error && !working && !reviewing && <DeskStatusNotice id={'test-workspace:'+owner} tone="warning" label={msg('Tests need attention')} actions={<Button onClick={()=>{attempted.current='';setRetry(n=>n+1);setError('');void storage.query.refetch()}}>{msg('Retry')}</Button>}>{error}</DeskStatusNotice>}
      {active && capabilityError && <DeskStatusNotice id="test-capabilities" tone="warning" label={msg('Test capabilities unavailable')} detail={capabilityError} actions={<Button onClick={()=>setRetry(n=>n+1)}>{msg('Retry')}</Button>}>{msg('Runtime test capabilities could not be checked. Cases and history remain available.')}</DeskStatusNotice>}
      {active && status==='ready' && supported===false && <DeskStatusNotice id="test-runtime" tone="warning" label={msg('Test runtime update needed')}>{msg('Update the runtime to run snapshot test suites. Cases and history remain available.')}</DeskStatusNotice>}
      <Dialog open={showSuiteHelp} onOpenChange={setShowSuiteHelp} title={msg('About cases and history')} footer={<DialogActions><Button onClick={()=>setShowSuiteHelp(false)}>{msg('Close')}</Button></DialogActions>}>
        <p>{msg('Cases and history are saved on this computer. Export a matrix to share saved expectations.')}</p>
        <p>{msg('Test runs do not change the pack. Results belong to the exact inputs and revision that ran.')}</p>
      </Dialog>
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
            <Button variant="danger" disabled={busy || deleteTyped.trim() !== (pendingDelete?.name || msg('Yes'))} onClick={() => void remove()}>
              {busy ? msg('Deleting…') : msg('Delete case')}
            </Button>
          </DialogActions>
        }
      >
        <p className={styles.deleteCaseName}>{pendingDelete?.name}</p><TypedConfirmation confirmation={pendingDelete?.name || msg('Yes')} value={deleteTyped} onChange={setDeleteTyped} disabled={busy} />
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
      <Dialog open={showCaseBrief && !!briefSubject} onOpenChange={setShowCaseBrief} title={msg('Case brief')} openerRef={briefOpener}
        footer={<DialogActions><Button onClick={() => setShowCaseBrief(false)}>{msg('Close')}</Button></DialogActions>}>
        {showCaseBrief && briefSubject && <BriefPane key={briefSubject.id} subject={briefSubject} embedded/>}
      </Dialog>
      {reviewing ? (
        <>
          <div className={styles.proposalList} hidden={reviewEditing}>
            <ProposalReview
              key={reviewing.id}
              cases={reviewRows}
              previousCases={suite.cases}
              intentionalTypes={intentionalFactTypes(reviewing.attempts.at(-1)?.document)}
              saved={(suite.proposals?.find((p) => p.id === reviewing.id) ?? reviewing).savedCases ?? {}}
              busy={busy}
              stale={!digest || reviewing.packDigest !== digest || status !== 'ready'}
              error={error}
              onEdit={async (c) => {
                if (await select(c, false)) {
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
          {requestedCase ? <section className={styles.casePage} aria-label={msg('Test case')}>
            <header className={styles.casePageHeader}>
              <nav aria-label={msg('Test case breadcrumb')}><Link to={casesHref()}>{msg('Tests')}</Link><IconChevronRight/><OverflowTooltip content={working?.name ?? requestedCase}><span aria-current="page">{working?.name || msg('New case')}</span></OverflowTooltip></nav>
              {caseResult?.run && !runView && <Button variant="quiet" disabled={busy} aria-label={msg('View last result')} onClick={()=>{setRunView(caseResult.run!);setSourceMenu(false)}}><IconHistory/>{msg(caseResult.label)}</Button>}
              {briefCase && <Button variant="quiet" ref={briefOpener} onClick={()=>setShowCaseBrief(true)}><IconInfo/>{msg('Case brief')}</Button>}
            </header>
            {working ? <div className={styles.casePageBody} data-result={!!runView||undefined}>{detailNode}</div> : storage.query.isPending ? <PageLoading>{msg('Loading tests…')}</PageLoading> : storage.query.error ? <CollectionEmpty title={msg('Tests unavailable')}>{null}</CollectionEmpty> : <CollectionEmpty title={msg('Case not found')}><p>{msg('This case may have been deleted or its unsaved draft is no longer available.')}</p><Link to={casesHref()}>{msg('Back to cases')}</Link></CollectionEmpty>}
          </section> : <BuilderSplit preference="pack" title={msg('Test result')} open={active && bottomOpen && !!runView} onClose={()=>setBottomOpen(false)} editor={detailNode}>
            {history ? <CollectionLayout label={msg('Test run history')} loading={storage.query.isPending&&!suite.runs.length?msg('Loading tests…'):undefined} toolbar={listHeader}>
              <div className={styles.history}>
                {suite.runs.length > 0 && <Disclosure title={msg('Compare runs and pack changes')}><RunComparison runs={suite.runs} currentText={text}/></Disclosure>}
                {storage.query.error&&!suite.runs.length ? <CollectionEmpty title={msg('Tests unavailable')}>{null}</CollectionEmpty> : !suite.runs.length && <CollectionEmpty title={msg('No runs yet')}><p>{msg('Run a case to save its result here.')}</p></CollectionEmpty>}
                {[...suite.runs].reverse().map(r=><Button variant="quiet" key={r.id} onClick={()=>{setRunView(r);setSourceMenu(false);details.reveal()}}>
                  <span>{new Date(r.at).toLocaleString()} · {r.origin === 'draft' ? msg('From draft') : msg('Tests')}</span>
                  <span>{r.error ? msg('Could not run') : r.trial ? msg('Exploratory') : r.packDigest !== digest ? msg('Historical') : r.report?.status}</span>
                </Button>)}
              </div>
            </CollectionLayout> : <TestsCollection suite={suite} digest={digest} query={query} onQuery={value=>{setQuery(value);updateListParam('testSearch',value,'')}} filter={filter} onFilter={value=>{setFilter(value);updateListParam('testResult',value,'all')}} sort={sort} onSort={value=>{setSort(value);updateListParam('testSort',value,'saved')}}
              busy={busy} canRun={status==='ready'&&supported===true} header={listHeader} caseHref={caseHref} onRun={cases=>void runCases(cases)}
              onResult={run=>{setRunView(run);setSourceMenu(false);details.reveal()}}
              onDuplicate={c=>{const id='case-'+crypto.randomUUID(); void select({...structuredClone(c),id,row:{...c.row,id},revision:0,name:c.name+' '+msg('copy'),intentionalFactTypes:c.intentionalFactTypes?.map(entry=>({...entry,caseId:id}))})}}
              onDelete={(c,opener)=>{deleteOpener.current=opener;setDeleteError('');setPendingDelete(structuredClone(c))}}
              onExport={exportCases} onReset={resetList} loading={storage.query.isPending} unavailable={!!storage.query.error&&!suite.cases.length}
              coverage={coverageRun ? missingProbes(coverageRun).length : 0} onCoverage={()=>{if(coverageRun){setRunView(coverageRun);details.reveal()}}} onDesign={()=>{if(coverageRun)designMissing(coverageRun)}}/>
            }
          </BuilderSplit>}

        </>
      )}
    </div>
  )
}
