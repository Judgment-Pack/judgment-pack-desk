import { describe, expect, it } from 'vitest'
import { Ledger } from './ledger'
import { admitCases, canCreateDraft, documentBasis, factPaths, statementsOf, readinessKey, traceCitations, matrixDocument, researchRecord, INITIAL_STATE, type RunState } from './run'
import type { AuthoringMode } from './mode'
import type { ChatAttachment } from '../chat/store'

const PAGE_URL = 'https://example.org/eligibility'

const PACK = {
  specVersion: '0.2.0-draft',
  id: 'https://example.org/packs/fswp-screening',
  version: '0.1.0',
  title: 'FSWP minimum requirements screening',
  decision: { intent: 'screen', question: 'Does the applicant meet the FSWP minimum requirements?' },
  sources: [
    {
      id: 'ircc-fswp',
      title: 'Express Entry: Federal Skilled Worker Program',
      locator: { kind: 'uri', value: PAGE_URL },
      publisher: 'Immigration, Refugees and Citizenship Canada',
      publishedAt: '2024-12-13',
      citation: { location: 'src-1#e1', excerpt: "We don't count any hours you work above 30 hours/week." }
    }
  ],
  outcomes: [
    { id: 'meets', label: 'Meets the minimum requirements' },
    { id: 'does-not-meet', label: 'Does not meet the minimum requirements' }
  ],
  rules: [
    {
      id: 'hours',
      description: 'At least 1,560 hours',
      when: { op: 'fact', path: '/work/hours', operator: 'greater-than-or-equal', value: '1560' },
      outcome: 'meets',
      onUnknown: 'escalate',
      sourceRefs: ['ircc-fswp']
    }
  ],
  fallbackOutcome: 'does-not-meet',
  escalation: { triggers: ['unknown'], target: { kind: 'human-role', name: 'Screening officer' } }
}

/** The same pack citing nothing, as a draft resting on what the person said. */
const UNCITED = {
  ...Object.fromEntries(Object.entries(PACK).filter(([key]) => key !== 'sources')),
  rules: PACK.rules.map(({ sourceRefs: _refs, ...rule }) => rule)
}

/** The rule asked with the basis the state's citations were traced on, for a test that moves nothing. */
const onItsBasis = (state: RunState, mode: AuthoringMode) => canCreateDraft(state, mode, state.tracedBasis ?? '')

const MEETS = { kind: 'outcome', outcomeId: 'meets', reasons: [], handoff: { state: 'none' } }
const MISSES = { kind: 'outcome', outcomeId: 'does-not-meet', reasons: [], handoff: { state: 'none' } }
/** Cases a chat draft's reviewer writes from the person's first message. */
const STATEMENT_CASES = { cases: [
  { id: 'at-the-threshold', facts: { work: { hours: '1560' } }, expectedDisposition: MEETS, expectationSource: 'you-1', rationale: 'You said 1,560 hours qualify.' },
  { id: 'just-under', facts: { work: { hours: '1559' } }, expectedDisposition: MISSES, expectationSource: 'you-1', rationale: 'One hour short does not.' }
] }
const CASES = {
  cases: [
    { id: 'meets-hours', facts: { work: { hours: '1560' } }, expectedDisposition: { kind: 'outcome', outcomeId: 'meets', reasons: [], handoff: { state: 'none' } }, expectationSource: 'src-1#e1', rationale: 'at the threshold' },
    { id: 'under-hours', facts: { work: { hours: '1559' } }, expectedDisposition: { kind: 'outcome', outcomeId: 'does-not-meet', reasons: [], handoff: { state: 'none' } }, expectationSource: 'src-1#e1', rationale: 'just under' },
    { id: 'hours-missing', facts: {}, expectedDisposition: { kind: 'unresolved', reasons: ['unknown'], handoff: { state: 'requested', triggeredBy: ['unknown'] } }, expectationSource: 'src-1#e1', rationale: 'missing fact escalates' },
    { id: 'ungrounded', facts: {}, expectedDisposition: { kind: 'outcome', outcomeId: 'meets', reasons: [], handoff: { state: 'none' } }, expectationSource: 'src-9#e9', rationale: 'no excerpt' }
  ]
}
describe('citation tracing and case admission', () => {
  it('traces only an excerpt the run recorded, with its text and URL', () => {
    const ledger = new Ledger('s1')
    ledger.open('page', { source: 'read', dialect: 'jina-reader', url: 'https://a.example/p' })
    ledger.settle('src-1', {
      response: { text: '', result: { kind: 'null' }, receipt: { kind: 'null' }, salts: {} },
      document: { url: 'https://a.example/p', title: 'P', text: 'A claim of $50 or less is approved.', pageDates: {} }
    })
    ledger.cite('src-1', 'A claim of $50 or less')
    const doc = (citation: unknown, value: unknown = 'https://a.example/p') => ({ sources: [{ id: 'p', locator: { kind: 'uri', value }, citation }] })
    // Unchecked: nothing traces until the receipt has verified.
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'A claim of $50 or less' }), ledger)[0]!.reason).toContain('has not been checked')
    ledger.verified('src-1', { state: 'verified', at: 't', keyId: 'k' })
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'A claim   of $50 or less' }), ledger)[0]!.traced).toBe(true)
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'something else' }), ledger)[0]!.reason).toContain('not the recorded excerpt text')
    expect(traceCitations(doc({ location: 'src-1#e2', excerpt: 'x' }), ledger)[0]!.reason).toContain('no excerpt src-1#e2')
    expect(traceCitations(doc({ location: 'section 4', excerpt: 'x' }), ledger)[0]!.reason).toContain('no excerpt id')
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'A claim of $50 or less' }, 'https://b.example/'), ledger)[0]!.reason).toContain('not the URL')
    expect(traceCitations(doc({ location: 'src-1#e1', excerpt: 'A claim of $50 or less' }, 42), ledger)[0]!.reason).toContain('not a string URL')
    expect(traceCitations({ sources: [{ id: 'p', citation: { location: 'src-1#e1', excerpt: 'A claim of $50 or less' } }] }, ledger)[0]!.traced).toBe(false)
    expect(traceCitations({ sources: 'no' }, ledger)).toEqual([])
    expect(factPaths(PACK)).toEqual(['/work/hours'])
  })
  it('never admits a case that would rewrite an established one', () => {
    const ledger = new Ledger('s1')
    const established = [CASES.cases[0]!] as never
    const { admitted, dropped } = admitCases({ cases: [{ ...CASES.cases[0]!, facts: { work: { hours: '1' } } }, { id: 'Bad Id' }] }, ledger, established)
    expect(admitted).toEqual([])
    expect(dropped.map((d) => d.reason)).toEqual(['a case with this id is already established and is never rewritten', 'the id is not kebab-case'])
    expect(admitCases(null, ledger, []).dropped[0]!.reason).toContain('without a cases array')
  })
})

const QUOTE='At least 1,560 hours of paid work in the last ten years.'
describe('historical conversation readers', () => {
  it('holds every mode to one rule over the same settled state', () => {
    const digest = 'fixture-digest'
    const row = { ...STATEMENT_CASES.cases[0]! }
    const base: RunState = { ...INITIAL_STATE, status: 'ready', phase: 'review', tracedBasis: 'basis-1', cases: [row],
      candidates: [{ revision: 1, producedBy: 'conversation', document: UNCITED, text: '{}', digest, check: { documentDigest: digest, valid: true, diagnostics: [], cases: [{ id: row.id, passed: true, expected: {}, actual: {} }] } }] }
    const settledAt = (state: RunState): RunState => ({ ...state, readiness: readinessKey(state) })
    const none = settledAt(base)
    // Cases join the rule in every mode: none, or one disagreeing, or one left
    // open for the person, and nothing is ready.
    const caseless = settledAt({ ...base, cases: [], candidates: [{ ...base.candidates[0]!, check: { ...base.candidates[0]!.check!, cases: [] } }] })
    expect([onItsBasis(caseless, 'draft'), onItsBasis(caseless, 'web-research')]).toEqual([false, false])
    const disagreeing = settledAt({ ...base, candidates: [{ ...base.candidates[0]!, check: { ...base.candidates[0]!.check!, cases: [{ id: row.id, passed: false, expected: {}, actual: {} }] } }] })
    expect(onItsBasis(disagreeing, 'draft')).toBe(false)
    expect(onItsBasis(settledAt({ ...base, expectationIssues: [{ id: 'open', original: row, message: 'invalid' }] }), 'draft')).toBe(false)
    expect([onItsBasis(none, 'draft'), onItsBasis(none, 'web-research'), canCreateDraft(none, 'research')]).toEqual([true, false, false])
    const traced = settledAt({ ...base, citations: [{ sourceId: 'a', location: 'attachment:x', excerptId: null, url: null, traced: true, reason: '', quote: 'q' }] })
    expect([onItsBasis(traced, 'draft'), onItsBasis(traced, 'web-research')]).toEqual([true, true])
    const open = settledAt({ ...base, citations: [...traced.citations, { sourceId: 'b', location: null, excerptId: null, url: null, traced: false, reason: 'r' }] })
    expect([onItsBasis(open, 'draft'), onItsBasis(open, 'web-research'), canCreateDraft(open, 'research')]).toEqual([false, false, false])
    // Asked with the basis standing now, a readiness reached on another is
    // none; asked with none, the answer is no.
    expect([canCreateDraft(none, 'draft', 'basis-1'), canCreateDraft(none, 'draft', 'basis-2'), canCreateDraft(none, 'draft')]).toEqual([true, false, false])
    // Research reads no such basis.
    expect(canCreateDraft({ ...none, citations: [{ sourceId: 'a', location: 'src-1#e1', excerptId: 'src-1#e1', url: null, traced: true, reason: '' }] }, 'research')).toBe(false)
    // While the citations are being traced they say nothing, and nothing is ready.
    expect(onItsBasis({ ...none, tracing: true }, 'draft')).toBe(false)
    expect(onItsBasis({ ...traced, tracing: true }, 'web-research')).toBe(false)
    // An invalid or stale check is never ready.
    expect(onItsBasis({ ...none, candidates: [{ ...none.candidates[0]!, check: { ...none.candidates[0]!.check!, valid: false } }] }, 'draft')).toBe(false)
    expect(onItsBasis({ ...none, candidates: [{ ...none.candidates[0]!, check: { ...none.candidates[0]!.check!, documentDigest: 'other' } }] }, 'draft')).toBe(false)
  })
  it('omits legacy mixed inputs without renumbering plain statements and rejects malformed saved statements', async () => {
    const { checkpoint, decodeCheckpoint } = await import('../chat/checkpoint')
    const legacy = { ...INITIAL_STATE, turns: [{ role: 'user' as const, kind: 'brief' as const, text: 'My requirement. Reference: hidden-rule', input: 'My requirement. Selected item reference: hidden-rule', at: '2026-10-01T00:00:00Z' }] }
    expect([...statementsOf(decodeCheckpoint(checkpoint(legacy, [])).state.turns)]).toEqual([])
    expect([...statementsOf([...legacy.turns, { role: 'user', kind: 'message', text: 'Plain words.', at: '' }])]).toEqual([['you-2', 'Plain words.']])
    expect(() => decodeCheckpoint(checkpoint({ ...legacy, turns: [{ ...legacy.turns[0]!, statement: 42 as never }] }, []))).toThrow('Saved chat data is not supported')
  })

  it('admits a statement or a traced page as a chat case\'s ground, and nothing else', () => {
    const ledger = new Ledger('unset')
    const traced = { sourceId: 'page', location: 'attachment:a/sha256:b/page/1', excerptId: null, url: null, traced: true, reason: '', quote: QUOTE }
    const grounds = { statements: new Map([['you-1', 'People with 1,560 hours qualify.']]), citations: [traced, { ...traced, sourceId: 'other', location: 'attachment:c/sha256:d/page/2', traced: false }] }
    const row = (id: string, expectationSource: string) => ({ ...STATEMENT_CASES.cases[0]!, id, expectationSource })
    const { admitted, dropped } = admitCases({ cases: [row('said', 'you-1'), row('quoted', traced.location), row('never-said', 'you-9'), row('untraced-page', 'attachment:c/sha256:d/page/2'), row('excerpt', 'src-1#e1')] }, ledger, [], grounds)
    expect(admitted.map(item => [item.id, item.expectationSource])).toEqual([['said', 'you-1'], ['quoted', traced.location]])
    expect(dropped).toEqual([
      { id: 'never-said', reason: 'expectationSource "you-9" is neither one of your messages nor a page the draft cites and traced' },
      { id: 'untraced-page', reason: 'expectationSource "attachment:c/sha256:d/page/2" is neither one of your messages nor a page the draft cites and traced' },
      { id: 'excerpt', reason: 'expectationSource "src-1#e1" is not an excerpt recorded in this run' }
    ])
    // Research admits neither: its cases rest on recorded excerpts only.
    expect(admitCases({ cases: [row('said', 'you-1')] }, ledger, []).dropped).toEqual([{ id: 'said', reason: 'expectationSource "you-1" is not an excerpt recorded in this run' }])
  })
  it('keys a trace to the pin and to every kept document identity and pages', () => {
    const file: ChatAttachment = {id:'12345678-1234-1234-1234-123456789012',name:'Policy',text:'',document:{id:'12345678-1234-1234-1234-123456789012',digest:'sha256:'+'a'.repeat(64),pages:[1],allowPartial:false}}
    const base=documentBasis({public:'a'},[file])
    expect(documentBasis({public:'a'},[file])).toBe(base)
    expect(documentBasis({public:'b'},[file])).not.toBe(base)
    expect(documentBasis({public:'a'},[])).not.toBe(base)
    expect(documentBasis({public:'a'},[{...file,document:{...file.document!,pages:[]}}])).not.toBe(base)
    expect(documentBasis({public:'a'},[{...file,document:{...file.document!,allowPartial:true}}])).not.toBe(base)
  })
})

it('retains statement-grounded matrix and record provenance without execution', () => {
 const state:RunState={...INITIAL_STATE,cases:[STATEMENT_CASES.cases[0]!],turns:[{role:'user',kind:'message',text:'People with 1,560 hours qualify.',statement:'People with 1,560 hours qualify.',at:''}]}
 const ledger=new Ledger('unset')
 const matrix=matrixDocument(state,ledger) as {cases:{focus:string;cites?:unknown}[]}
 expect(matrix.cases[0]).toMatchObject({focus:'You said 1,560 hours qualify. [you-1]'})
 expect(matrix.cases[0]?.cites).toBeUndefined()
 const record=researchRecord(state,ledger,'abc') as {statements:unknown;digests:{means:Record<string,string>}}
 expect(record.statements).toEqual([{id:'you-1',text:'People with 1,560 hours qualify.'}])
 expect(Object.keys(record.digests.means)).toEqual(['/packSha256','/checkedCandidateSha256','/expectationIssues/*/proposal/candidateDigest'])
})

// Historical readers still verify quoted pages; they do not own an execution.
it('traces only current verified selected pages and withholds unavailable, partial or mismatched material', async () => {
 const {tracePageCitations}=await import('./run')
 const {default:fixture}=await import('../documents/__fixtures__/web-snapshot.json')
 const record=structuredClone(fixture) as unknown as import('../documents/record').DocumentRecord
 const page=record.content.pages[0]!
 const quote=page.text.slice(0,Math.min(60,page.text.length))
 const reference={id:'12345678-1234-1234-1234-123456789012',digest:'sha256:'+'a'.repeat(64),pages:[1],allowPartial:false}
 const file:ChatAttachment={id:reference.id,name:'Policy',text:'',document:reference}
 const verified={record,digest:reference.digest,object:{version:1,original:{name:'Policy',mediaType:'text/plain',bytes:'',sha256:record.document.id}}} satisfies import('../documents/client').VerifiedDocument
 const url=record.provenance.source.kind==='web'?record.provenance.source.url:PAGE_URL
 const location=`attachment:${reference.id}/${reference.digest}/page/1`
 const document=(where=location,text=quote,locator=url)=>({sources:[{id:'policy',locator:{kind:'uri',value:locator},citation:{location:where,excerpt:text}}]})
 const signal=new AbortController().signal
 const trace=async(doc:unknown,files=[file],load=async()=>verified)=>tracePageCitations(doc,files,load,signal)
 expect((await trace(document()))[0]?.traced).toBe(true)
 expect((await trace(document(location,`“${quote}”`)))[0]?.traced).toBe(true)
 for(const doc of [document('src-1#e1'),document(location.replace('/page/1','/page/2')),document(location,'a quote absent from this page'),document(location,quote,'https://different.invalid')])expect((await trace(doc))[0]?.traced).toBe(false)
 expect((await trace(document(),[]))[0]?.traced).toBe(false)
 expect((await trace(document(),[file],async()=>{throw Error('Current pin mismatch')}))[0]?.traced).toBe(false)
 const partial=structuredClone(verified);partial.record.processing.status='partial'
 expect((await trace(document(),[file],async()=>partial))[0]?.traced).toBe(false)
 const stop=new AbortController();stop.abort()
 await expect(tracePageCitations(document(),[file],async()=>verified,stop.signal)).rejects.toMatchObject({name:'AbortError'})
})
