/** Reviewable fixture mock. This entry is separate from the production route. */
import { useRef, useState, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { Button } from '../ui/Button'
import { TextArea } from '../ui/TextArea'
import { Select } from '../ui/Select'
import { SegmentedControl } from '../ui/SegmentedControl'
import { Tooltip, TooltipProvider } from '../ui/Tooltip'
import { ConditionTree } from '../packs/document/ConditionTree'
import { IconCheck, IconChevronDown, IconGear, IconGraph, IconHelp, IconMatrix, IconPack, IconPanelBottom, IconPanelRight, IconPlus } from '../shell/icons'
import '../styles.css'
import styles from './AuthoringPreview.module.css'

type Screen = 'initial' | 'active' | 'review'
const parameters = new URLSearchParams(location.search)
document.documentElement.dataset.theme = parameters.get('theme') === 'light' ? 'light' : 'dark'
document.documentElement.dataset.density = parameters.get('density') === 'compact' ? 'compact' : 'comfortable'
const policy = 'Create an expense reimbursement pack. Approve claims up to $50. Above $50, require a receipt. Claims over $1,000 should go to Finance.'

function IconButton({ label, children, onClick }: { label: string; children: ReactNode; onClick?: () => void }) {
  return <Tooltip content={label}><Button variant="quiet" aria-label={label} onClick={onClick}>{children}</Button></Tooltip>
}

function Composer({ initial = false, running, onSend, onStop }: { initial?: boolean; running?: boolean; onSend: () => void; onStop: () => void }) {
  const [text, setText] = useState(initial ? policy : '')
  const [mode, setMode] = useState('draft')
  const [model, setModel] = useState('gemini')
  const [files, setFiles] = useState<string[]>(initial ? ['Expense policy.pdf'] : [])
  const file = useRef<HTMLInputElement>(null)
  return <div className={styles.composer} onDragOver={event => event.preventDefault()} onDrop={event => {
    event.preventDefault(); setFiles(previous => [...previous, ...Array.from(event.dataTransfer.files, item => item.name)])
  }}>
    {!!files.length && <div className={styles.files}>{files.map((name, index) => <span key={`${name}-${index}`}>{name}
      <button aria-label={`Remove ${name}`} onClick={() => setFiles(files.filter((_, at) => index !== at))}>×</button></span>)}</div>}
    <label className={styles.srOnly} htmlFor={initial ? 'initial-message' : 'message'}>{initial ? 'Describe the decision' : 'Message the assistant'}</label>
    <TextArea id={initial ? 'initial-message' : 'message'} value={text} placeholder="Ask a question or describe a change…"
      onChange={event => setText(event.target.value)} onKeyDown={event => {
        if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing && text.trim()) { event.preventDefault(); onSend() }
      }} />
    <div className={styles.composerTools}>
      <input ref={file} type="file" multiple hidden onChange={event => setFiles(previous => [...previous, ...Array.from(event.target.files ?? [], item => item.name)])} />
      <IconButton label="Attach a reference" onClick={() => file.current?.click()}><IconPlus /></IconButton>
      <div className={styles.mode}><label className={styles.srOnly} htmlFor="authoring-mode">Task mode</label><Select id="authoring-mode" value={mode} onValueChange={setMode}
        options={[{ value: 'draft', label: 'Draft' }, { value: 'ask', label: 'Ask' }, { value: 'challenge', label: 'Challenge' }]} /></div>
      <div className={styles.model}><label className={styles.srOnly} htmlFor="authoring-model">Model</label><Select id="authoring-model" value={model} onValueChange={setModel}
        options={[{ value: 'gemini', label: 'Gemini 2.5 Pro' }, { value: 'flash', label: 'Gemini 2.5 Flash' }]} /></div>
      <span className={styles.grow} />
      <Button variant={running ? 'secondary' : 'primary'} disabled={!running && !text.trim()} onClick={running ? onStop : onSend}>{running ? 'Stop' : initial ? 'Start drafting' : 'Send'}</Button>
    </div>
  </div>
}

function Rule({ title, path, operator, value, outcome }: { title: string; path: string; operator: string; value: unknown; outcome: string }) {
  return <section className={styles.rule}><h3>{title}</h3><ConditionTree readOnly structured condition={{ op: 'fact', path, operator, value }} at="/preview" />
    <div className={styles.result}><span>Result</span><strong>{outcome}</strong></div></section>
}

function Preview() {
  const [screen, setScreen] = useState<Screen>((['active', 'review'].includes(parameters.get('state') ?? '') ? parameters.get('state') : 'initial') as Screen)
  const [tab, setTab] = useState(screen === 'review' ? 'changes' : 'preview')
  const [mobile, setMobile] = useState('draft')
  const [activity, setActivity] = useState(false)
  const [paused, setPaused] = useState(false)
  const [notice, setNotice] = useState('')
  const start = () => { setScreen('active'); setTab('preview'); setMobile('chat'); setPaused(false) }
  const review = () => { setScreen('review'); setTab('changes'); setPaused(false) }
  return <TooltipProvider><div className={styles.app}>
    <header className={styles.topbar}><span className={styles.identity}>JD</span><strong>judgment-pack desk</strong><span className={styles.project}>This project <IconChevronDown /></span>
      <span className={styles.grow} /><IconButton label="Show assistant" onClick={() => setMobile('chat')}><IconPanelRight /></IconButton>
      <IconButton label="Toggle activity" onClick={() => setActivity(!activity)}><IconPanelBottom /></IconButton><span className={styles.avatar}>LU</span><span>local user</span></header>
    <nav className={styles.rail} aria-label="Main navigation"><Button variant="quiet" className={styles.selected} onClick={() => { setScreen('initial'); setNotice('') }}><IconPlus />Create pack</Button>
      <Button variant="quiet" onClick={() => setNotice('Design preview: pack navigation is outside this mock.')}><IconPack />Packs<span className={styles.grow} />2</Button><hr />
      <Button variant="quiet" onClick={() => setNotice('Design preview: matrix navigation is outside this mock.')}><IconMatrix />Matrix and coverage</Button>
      <Button variant="quiet" onClick={() => setNotice('Design preview: graph navigation is outside this mock.')}><IconGraph />Graphs</Button>
      <span className={styles.grow} /><Button variant="quiet" onClick={() => setNotice('Models and tools are configured in Admin.')}><IconGear />Admin</Button>
      <Button variant="quiet" onClick={() => setNotice('Describe one decision, add policy references, then review the proposed rules and tests.')}><IconHelp />Help & About</Button></nav>
    <div className={styles.workspace}>
      <header className={styles.pageHeader}><div className={styles.breadcrumb}>Packs <span>/</span> <strong>Create pack</strong></div><span className={styles.grow} />
        {screen !== 'initial' && <><span className={styles.saved}>Draft · Revision {screen === 'active' ? '4' : '5'}</span>
          <Button variant="quiet" onClick={review}>Review draft</Button><Button variant="primary" disabled={screen !== 'review'} onClick={() => setNotice('Design preview: no pack was created.')}>Create pack</Button></>}
      </header>
      {notice && <div className={styles.notice} role="status">{notice}<Button variant="quiet" onClick={() => setNotice('')}>Dismiss</Button></div>}
      {screen === 'initial' ? <main className={styles.initial}><div className={styles.intro}>
        <span className={styles.introIcon}><IconPack /></span><h1>What decision should this pack help make?</h1>
        <p>Describe the outcome you need. Your assistant will build the rules,<br className={styles.wideOnly} /> test the edge cases, and help you review the draft.</p>
        <Composer initial onSend={start} onStop={() => {}} />
        <div className={styles.introFooter}><span>References help the assistant stay grounded in your policy.</span><Button variant="quiet" onClick={() => setNotice('Include who the policy applies to, possible outcomes, exceptions and when a person should decide.')}>Writing guide</Button></div>
        <div className={styles.suggestions}><span>Try a starting point</span>{['Expense approval', 'Investigation triage', 'Access requests'].map(label => <Button key={label} variant="quiet" onClick={start}>{label} →</Button>)}</div>
      </div></main> : <>
        <div className={styles.mobileTabs}><SegmentedControl label="Workspace view" value={mobile} onValueChange={setMobile} segments={[{ value: 'draft', label: 'Draft' }, { value: 'chat', label: 'Chat' }]} /></div>
        <div className={styles.split} data-mobile={mobile}>
          <main className={styles.draft}><header className={styles.draftHeader}><div className={styles.titleRow}><h1>Expense reimbursement</h1><span>{screen === 'active' ? paused ? 'Paused' : 'Checking draft' : 'Ready for review'}</span></div>
            <p>May this expense reimbursement claim be approved?</p><nav className={styles.tabs} aria-label="Draft views">{['preview', 'changes', 'tests', 'sources'].map(item =>
              <button key={item} aria-current={tab === item ? 'page' : undefined} onClick={() => setTab(item)}>{item[0].toUpperCase() + item.slice(1)}{item === 'tests' && <span>{screen === 'review' ? '7' : '6'}</span>}</button>)}</nav></header>
            <div className={styles.document}>
              {tab === 'preview' && <><section className={styles.about}><h2>When this pack applies</h2><ConditionTree readOnly structured condition={{ op: 'fact', path: '/claim/type', operator: 'equals', value: 'expense-reimbursement' }} at="/applicability" /></section>
                <h2>Decision rules</h2><Rule title="Small claims" path="/claim/amount" operator="less-than-or-equal" value="50" outcome="Approve" />
                <section className={styles.rule}><h3>Claims above $50</h3><div className={styles.result}><span>Receipt attached</span><strong>Approve</strong></div><div className={styles.result}><span>No receipt attached</span><strong>Decline</strong></div><p>Receipt availability comes from the supplied claim facts.</p></section>
                <Rule title="Large claims" path="/claim/amount" operator={screen === 'active' ? 'greater-than-or-equal' : 'greater-than'} value="1000" outcome="Hand off to Finance" />
                <p className={styles.sourceNote}>Source: Expense policy.pdf · Reimbursement limits</p></>}
              {tab === 'changes' && <><div className={styles.sectionLead}><h2>What changed</h2><span>4 repairs across 5 revisions</span></div><p>The draft now preserves the policy’s exact boundaries and receipt requirement.</p>
                {[['Decision question', 'Restored the question this pack answers.'], ['Claims of exactly $50', 'Included in the small-claim approval rule.'], ['Claims above $50 without a receipt', 'Decline instead of approve.'], ['Claims of exactly $1,000', 'Evaluate the receipt rule. Only higher amounts go to Finance.']].map(([title, body]) => <section className={styles.change} key={title}><IconCheck /><div><h3>{title}</h3><p>{body}</p></div></section>)}
                <div className={styles.testSummary}><IconCheck /><div><strong>7 of 7 test cases agree with their expectations</strong><p>Checked against this candidate. No policy questions remain in this example.</p></div></div>
                <Button onClick={() => setTab('tests')}>View test results</Button></>}
              {tab === 'tests' && <><div className={styles.sectionLead}><h2>Test results</h2><span>Revision 5</span></div><p>Expected answers were established from the policy before testing.</p>
                <div className={styles.testTable} role="table" aria-label="Test cases"><div role="row"><strong role="columnheader">Case</strong><strong role="columnheader">Expected</strong><strong role="columnheader">Result</strong></div>
                  {[['$10 without receipt', 'Approve'], ['$50 without receipt', 'Approve'], ['$75 without receipt', 'Decline'], ['$75 with receipt', 'Approve'], ['$1,000 with receipt', 'Approve'], ['$1,000.01 with receipt', 'Hand off'], ['Amount missing', 'Unresolved']].map(([label, outcome]) => <div role="row" key={label}><span role="cell">{label}</span><span role="cell">{outcome}</span><span role="cell" className={styles.passed}>Passed</span></div>)}</div><p className={styles.sourceNote}>Rehearsal only · No operational decision was recorded</p></>}
              {tab === 'sources' && <><h2>References</h2><section className={styles.rule}><h3>Expense policy.pdf</h3><p>Policy reference · Reimbursement limits</p><blockquote>A claim of $50 or less is approved. Above $50, a receipt is required. Any claim above $1,000 is handed to Finance.</blockquote></section>
                <p className={styles.sourceNote}>The draft uses policy references for its rules. Historical cases are kept separately for testing.</p></>}
            </div>
          </main>
          <aside className={styles.chat} aria-label="Authoring conversation"><header className={styles.chatHeader}><strong>Assistant</strong><span className={styles.grow} /><span>{screen === 'review' ? 'Review complete' : paused ? 'Paused' : 'Working'}</span></header>
            <div className={styles.thread}><div className={styles.userMessage}>{policy}<div className={styles.attachment}>Expense policy.pdf</div></div>
              <article className={styles.assistantMessage}><strong>Assistant</strong><p>I’ll turn this into one decision, then test the receipt rules and the exact $50 and $1,000 boundaries.</p></article>
              <div className={styles.progress}><div><IconCheck /><span>Read the policy reference</span></div><div><IconCheck /><span>Drafted rules and Finance handoff</span></div><div><IconCheck /><span>Fixed the $50 boundary and receipt rule</span></div>
                <div>{screen === 'review' ? <IconCheck /> : <span className={styles.dot} />}<span>{screen === 'review' ? 'Checked the $1,000 boundary and missing facts' : 'Checking the exact $1,000 boundary'}</span></div></div>
              <article className={styles.assistantMessage}><strong>{screen === 'review' ? 'Ready for your review' : 'One boundary needs a correction'}</strong>
                <p>{screen === 'review' ? 'All 7 cases agree with their expected results. I made four repairs over five revisions, including the case where the amount is missing.' : 'The current draft hands a $1,000 claim to Finance. Your policy says “above $1,000”, so I’m correcting that boundary and rerunning every case.'}</p>
                {screen === 'review' ? <Button onClick={() => { setTab('changes'); setMobile('draft') }}>Review changes</Button> : <span className={styles.runMeta}>Revision 4 · 5 of 6 cases passed</span>}</article>
            </div>
            <footer className={styles.chatFooter}><div className={styles.context}><span>Expense reimbursement</span><span>Revision {screen === 'review' ? '5' : '4'}</span></div><Composer running={screen === 'active' && !paused} onSend={screen === 'active' ? review : start} onStop={() => setPaused(true)} />
              <div className={styles.chatOptions}><Button variant="quiet" onClick={() => setNotice('Thinking effort is model-dependent. Challenge draft is an independent review mode.')}>Thinking: Default</Button><Button variant="quiet" onClick={() => setNotice('Available tools: Read schema, Read examples, Validate draft, Rehearse test cases. No live gateway actions.')}>Tools · 4</Button></div>
            </footer>
          </aside>
        </div>
      </>}
      {activity && <section className={styles.activity}><strong>Activity</strong><p>Validate candidate → compare expected and actual results → repair → rerun all cases</p><p>Checkpoints preserve completed revisions when a run is interrupted.</p></section>}
    </div>
    <footer className={styles.statusbar}><span>Design preview · Fixture content</span><span className={styles.grow} /><Button variant="quiet" onClick={() => { document.documentElement.dataset.theme = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark' }}>Switch theme</Button>
      <Button variant="quiet" onClick={() => { setScreen('initial'); setNotice('') }}>Initial</Button><Button variant="quiet" onClick={start}>Drafting</Button><Button variant="quiet" onClick={review}>Review</Button></footer>
  </div></TooltipProvider>
}

createRoot(document.getElementById('root')!).render(<Preview />)
