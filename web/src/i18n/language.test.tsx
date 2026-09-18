import { CoverageReport } from '../components/CoverageReport'
import { TargetSide } from '../components/TargetPair'
import { MemoryRouter } from 'react-router-dom'
import { PackDocumentView } from '../packs/document/PackDocumentView'
import { ReferencesTab } from '../packs/inspector/ReferencesTab'
import { referencesFor } from '../packs/references'
import minimalPack from '../packs/__fixtures__/minimal.pack.json'
import type { PackDocument } from '../mcp/types'
import { NoSession, NO_SESSION_MESSAGE } from '../mcp/session'
import { ErrorBox } from '../components/primitives'
import { walkFallbackReason, readGraphDocument, edgeCarries } from '../mcp/graphDocument'
import { thinkingNotice, alwaysFromRefusal } from '../assistant/thinking'
import { listingRefusal } from '../assistant/modelListing'
import { describeEvent } from '../assistant/EventList'
import { checkpoint, decodeCheckpoint } from '../chat/checkpoint'
import { INITIAL_STATE } from '../research/run'
import { Conversation } from '../research/ui/Conversation'
import { IdentityProvider, useIdentity } from '../identity/IdentityProvider'
import { DeskConfigFixture } from '../config/DeskConfigProvider'
import { decodeDeskConfig, effectiveConfig } from '../config/deskConfig'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { initializeLanguage, i18n, LANGUAGE_KEY, language, languagePreference, languageReady, formatDate, formatNumber, msg, setLanguage, systemMessage, useLocale } from './index'
import { Message } from './Message'
import { currentLanguage } from './locales'
import { sourceMessage } from './source'
import { layersReached } from '../packs/checks'
import { checkLine } from '../assistant/endpointCheck'
import { CodeBlock } from '../ui/CodeBlock'
import { slugFor } from '../packs/newPack'
import { CREATE_REFUSALS } from '../packs/createRefusal'
import { readDraft } from '../assistant/proposalDiff'
import { StatusLine } from '../admin/SourceCard'

afterEach(async () => { cleanup(); vi.restoreAllMocks(); localStorage.clear(); setLanguage('en'); await languageReady(); localStorage.clear() })

describe('personal language preference', () => {
  it('localizes graph and absent-target captions while preserving colliding node and recipient names', async () => {
    const { container } = render(<>
      <CoverageReport groupByNode coverage={[
        { probe: 'node:the graph:outcome:Save', status: 'missing' },
        { probe: 'edge:0', status: 'missing' }
      ]} />
      <TargetSide label="Null target" member="null" />
      <TargetSide label="Raw recipient" member={JSON.stringify({ name: 'no target', kind: 'queue' })} />
    </>)
    await act(async () => { setLanguage('de'); await languageReady() })
    expect([...container.querySelectorAll('.coverage-group-title')].map(el => el.textContent)).toEqual(['the graph', msg('The graph')])
    expect(screen.getByText('kein Empfänger')).toBeTruthy()
    expect(screen.getByText('no target (queue)')).toBeTruthy()
  })

  it('updates optional pack headings and reference captions without rewriting document identifiers', async () => {
    const document = structuredClone(minimalPack) as PackDocument
    document.rules[0]!.outcome = 'Save'
    const references = referencesFor(document, '/rules/0')
    const { container } = render(<MemoryRouter>
      <PackDocumentView document={document} active={null} />
      <ReferencesTab references={references} packId="Save" />
    </MemoryRouter>)
    const article = container.querySelector('article')
    const referenceRow = screen.getByText('no declared outcome carries this id').closest('li')
    expect(referenceRow).toBeTruthy()
    await act(async () => { setLanguage('ja'); await languageReady() })
    expect(container.querySelector('article')).toBe(article)
    expect(container.querySelector('[data-pointer="/applicability"]')?.textContent).toContain(msg('When this pack applies'))
    expect(container.querySelector('[data-pointer="/fallbackOutcome"]')?.textContent).toContain(msg('Fallback outcome'))
    expect(screen.getByText('この ID を持つ結果は宣言されていません').closest('li')).toBe(referenceRow)
    expect(screen.getAllByText('Save').length).toBeGreaterThan(0)
    expect(references[0]?.id).toBe('Save')
    expect(document.rules[0]!.outcome).toBe('Save')
  })

  it('uses browser preferences, persists only an explicit choice, and observes system changes', async () => {
    const langs = vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['fr-CA', 'en'])
    const release = initializeLanguage(); await languageReady()
    expect(language()).toBe('fr')
    expect(localStorage.getItem(LANGUAGE_KEY)).toBeNull()
    expect(document.documentElement.lang).toBe('fr')
    expect(msg('Save')).toBe('Enregistrer')
    setLanguage('ja'); await languageReady()
    expect(currentLanguage()).toBe('ja')
    expect(localStorage.getItem(LANGUAGE_KEY)).toBe('ja')
    langs.mockReturnValue(['de-DE'])
    window.dispatchEvent(new Event('languagechange')); await languageReady()
    expect(language()).toBe('ja')
    setLanguage('system'); await languageReady()
    expect(language()).toBe('de')
    expect(localStorage.getItem(LANGUAGE_KEY)).toBeNull()
    langs.mockReturnValue(['ko-KR'])
    window.dispatchEvent(new Event('languagechange')); await languageReady()
    expect(language()).toBe('ko')
    release()
  })
  it('ignores unsupported or corrupt stored choices', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['xx', 'es-ES'])
    localStorage.setItem(LANGUAGE_KEY, '{broken')
    const release = initializeLanguage(); await languageReady()
    expect(language()).toBe('es')
    expect(languagePreference()).toBe('system')
    release()
  })
  it('applies the language for this visit when storage is blocked', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked') })
    expect(setLanguage('fr')).toBe(false); await languageReady()
    expect(language()).toBe('fr')
  })
  it('switches labels without remounting or translating the user’s draft', async () => {
    function Editor() {
      useLocale()
      const [value, setValue] = useState('Save <script>alert(1)</script>')
      return <><label>{msg('Description')}<input value={value} onChange={event => setValue(event.target.value)} /></label><button>{msg('Save')}</button></>
    }
    render(<Editor />)
    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'My unsent message' } })
    await act(async () => { setLanguage('fr'); await languageReady() })
    expect(screen.getByRole('button', { name: 'Enregistrer' })).toBeTruthy()
    expect(screen.getByRole('textbox')).toBe(input)
    expect((input as HTMLInputElement).value).toBe('My unsent message')
  })
  it('reorders inline content without translating or interpreting it as markup', async () => {
    i18n.addResource('fr', 'translation', 'Review <0/> with <1/>.', 'Avec <1/>, révisez <0/>.')
    setLanguage('fr'); await languageReady()
    render(<p><Message text="Review <0/> with <1/>." slots={[<code>{'Save<script>secret</script>'}</code>, <a href="/packs">Alice</a>]} /></p>)
    expect(screen.getByRole('link').getAttribute('href')).toBe('/packs')
    expect(document.querySelector('p')?.textContent).toBe('Avec Alice, révisez Save<script>secret</script>.')
    expect(document.querySelector('script')).toBeNull()
  })
  it('updates an open window when another window changes the preference', async () => {
    const release = initializeLanguage(); await languageReady()
    localStorage.setItem(LANGUAGE_KEY, 'pt-BR')
    window.dispatchEvent(new StorageEvent('storage', { key: LANGUAGE_KEY, newValue: 'pt-BR' })); await languageReady()
    expect(language()).toBe('pt-BR')
    release()
  })
  it('retains regional date and number conventions in system mode', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-GB'])
    const release = initializeLanguage(); await languageReady()
    const date = new Date(2026, 8, 17)
    expect(formatDate(date)).toBe(new Intl.DateTimeFormat('en-GB').format(date))
    setLanguage('de'); await languageReady()
    expect(formatNumber(12345.6)).toBe(new Intl.NumberFormat('de').format(12345.6))
    release()
  })
  it('uses locale plural rules rather than English word fragments', async () => {
    setLanguage('en'); await languageReady()
    expect(msg('Work · {{count}} steps', { count: 1 })).toBe('Work · 1 step')
    expect(msg('Work · {{count}} steps', { count: 2 })).toBe('Work · 2 steps')
    setLanguage('fr'); await languageReady()
    expect(msg('Work · {{count}} steps', { count: 0 })).toBe('Travail · 0 étape')
    expect(msg('Work · {{count}} steps', { count: 2 })).toBe('Travail · 2 étapes')
  })
  it('keeps the latest language choice when two catalogues are requested together', async () => {
    setLanguage('it')
    const previous = languageReady()
    setLanguage('es')
    await Promise.all([previous, languageReady()])
    expect(language()).toBe('es')
    expect(document.documentElement.lang).toBe('es')
  })

  it('localizes saved Desk notices at display time while keeping their original values', async () => {
    const file = 'Save <script>source</script>.txt'
    const source = sourceMessage('{{value0}} is not a text file.', { value0: file })
    setLanguage('fr'); await languageReady()
    expect(source).toBe(`${file} is not a text file.`)
    expect(systemMessage(source)).toBe(`${file} n’est pas un fichier texte.`)
    setLanguage('de'); await languageReady()
    expect(systemMessage(source)).toBe(`${file} ist keine Textdatei.`)
    expect(sourceMessage('{{value0}} is not a text file.', { value0: file })).toBe(source)
    expect(systemMessage('Provider diagnostic: Save')).toBe('Provider diagnostic: Save')
  })

  it('updates existing copy feedback without changing the copied source', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', Object.assign(Object.create(navigator), { clipboard: { writeText } }))
    render(<CodeBlock text={'{"outcomeId":"Save"}'} />)
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Copy JSON' })) })
    expect(screen.getByRole('status').textContent).toBe('Copied')
    await act(async () => { setLanguage('fr'); await languageReady() })
    expect(screen.getByRole('status').textContent).toBe('Copié')
    await act(async () => { setLanguage('ja'); await languageReady() })
    expect(screen.getByRole('status').textContent).toBe(msg('Copied'))
    expect(writeText).toHaveBeenCalledExactlyOnceWith('{"outcomeId":"Save"}')
    expect(document.querySelector('pre code')?.textContent).toBe('{"outcomeId":"Save"}')
    vi.unstubAllGlobals()
  })

  it('localizes persisted count messages with the new locale’s grammar', async () => {
    const key = '{{count}} excerpts'
    const savedOne = sourceMessage(key, { count: 1 })
    const savedZero = sourceMessage(key, { count: 0 })
    expect(savedOne).toBe('1 excerpt')
    expect(savedZero).toBe('0 excerpts')
    setLanguage('fr'); await languageReady()
    expect(systemMessage(savedOne)).toBe('1 extrait')
    expect(systemMessage(savedZero)).toBe('0 extrait')
    setLanguage('de'); await languageReady()
    expect(systemMessage(savedZero)).toBe('0 Auszüge')
    setLanguage('ja'); await languageReady()
    expect(systemMessage(savedOne)).toBe('抜粋 1 件')
    expect(sourceMessage(key, { count: 1 })).toBe(savedOne)
  })

  it('translates validation narration but preserves the runtime’s exact layer and status names', async () => {
    const report = { status: 'unsupported', layers: [{ name: 'carrier', status: 'passed' }], diagnostics: [] }
    setLanguage('fr'); await languageReady()
    const text = layersReached(report, msg).text
    expect(text).toContain('unsupported — carrier passed, 0 diagnostic.')
    expect(text).toContain('Les couches structural et semantic n’ont pas été exécutées.')
    expect(layersReached(report).text).toContain('structural and semantic layers did not run')
  })

  it('translates connection feedback without changing the probe or its quoted refusal', async () => {
    const probe = { reachable: true, status: 200, latencyMs: 10, diagnostic: '' }
    const answer = { of: 'gemini\nhttps://example.test', asking: false, probe, rows: [] }
    setLanguage('fr'); await languageReady()
    expect(checkLine(answer, msg).says).toBe('Connecté · aucun modèle indiqué par le point de terminaison')
    expect(checkLine({ ...answer, listingRefusal: 'Provider message: unauthorized' }, msg).quoted).toBe('Provider message: unauthorized')
    setLanguage('ja'); await languageReady()
    expect(checkLine(answer, msg).says).toBe('接続済み · エンドポイントからモデルの一覧なし')
    expect(answer.probe).toBe(probe)
  })

  it('localizes deferred creation failures while retaining canonical diagnostics and user values', async () => {
    const name = slugFor('123 example')
    expect('problem' in name).toBe(true)
    const original = 'problem' in name ? name.problem : ''
    const draft = readDraft('[]')
    expect('problem' in draft).toBe(true)
    setLanguage('ja'); await languageReady()
    expect(systemMessage(original)).toBe('名前は英字で始める必要があります。')
    expect(systemMessage(CREATE_REFUSALS['outside-root']!)).toBe('その場所はプロジェクトの外部です。何も作成していません。')
    if ('problem' in draft) expect(systemMessage(draft.problem)).toBe('エディターのデータは JSON ですがオブジェクトではありません')
    expect(slugFor('123 example')).toEqual(name)
    expect(systemMessage(sourceMessage('This project already has a pack called {{value0}}.', { value0: 'Save-原文' }))).toContain('Save-原文')
  })

  it('localizes configuration constraints without changing validation, paths or rejected values', async () => {
    const text = JSON.stringify({ deskConfigVersion: 1, organization: { name: 42 } })
    const decoded = decodeDeskConfig(text, 'project')
    expect(decoded.problems).toEqual([{ key: 'organization.name', reason: 'must be a string or null; found number 42' }])
    render(<StatusLine status={{ state: 'refused', problems: decoded.problems }} />)
    await act(async () => { setLanguage('fr'); await languageReady() })
    expect(screen.getByText('organization.name: doit être une chaîne ou null ; valeur trouvée : number 42')).toBeTruthy()
    expect(decodeDeskConfig(text, 'project')).toEqual(decoded)
    expect(systemMessage('Provider diagnostic: keep this exact')).toBe('Provider diagnostic: keep this exact')
  })

})


it('localizes the default identity without changing a configured display name', async () => {
  function IdentityLabel() { const identity = useIdentity(); return <span>{identity.displayName}</span> }
  const { rerender } = render(<DeskConfigFixture value={effectiveConfig(undefined)}><IdentityProvider><IdentityLabel /></IdentityProvider></DeskConfigFixture>)
  await act(async () => { setLanguage('fr'); await languageReady() })
  expect(screen.getByText(msg('local user'))).toBeTruthy()
  const explicit = effectiveConfig(decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1, user: { displayName: 'local user' } }), 'project'))
  rerender(<DeskConfigFixture value={explicit}><IdentityProvider><IdentityLabel /></IdentityProvider></DeskConfigFixture>)
  expect(screen.getByText('local user')).toBeTruthy()
  const inherited = effectiveConfig(decodeDeskConfig(JSON.stringify({ deskConfigVersion: 1, user: {} }), 'project'))
  rerender(<DeskConfigFixture value={inherited}><IdentityProvider><IdentityLabel /></IdentityProvider></DeskConfigFixture>)
  expect(screen.getByText(msg('local user'))).toBeTruthy()
})

it('localizes nested agent notices and retained paragraphs without translating identifiers', async () => {
  const notice = thinkingNotice('always', alwaysFromRefusal(400))
  const refused = listingRefusal(503)
  setLanguage('fr'); await languageReady()
  expect(systemMessage(notice)).toBe('ce modèle raisonne toujours : le point de terminaison a répondu 400 à toutes les variantes du paramètre désactivant le raisonnement')
  expect(systemMessage(refused)).toBe('la liste des modèles a été refusée — réponse 503, Desk a déjà atteint sa limite de requêtes simultanées vers ce point de terminaison')
  expect(describeEvent({ type: 'thinking_unavailable', detail: notice })).toBe(systemMessage(notice))
  expect(describeEvent({ type: 'thinking_unavailable', detail: notice }, sourceMessage)).toBe(notice)
  expect(systemMessage('Stopped. The last completed stage is kept.\n\nProvider diagnostic: exact')).toBe('Arrêté. La dernière étape terminée est conservée.\n\nProvider diagnostic: exact')
  expect(systemMessage(sourceMessage('verify: {{value0}} — {{value1}} ({{value2}})', { value0: 'Save', value1: 'FAILED', value2: 'signature-mismatch' }))).toBe('vérification : Save — FAILED (signature-mismatch)')
})

it('persists interrupted prose verbatim and translates only its separate annotation', async () => {
  const state = { ...INITIAL_STATE, status: 'stopped' as const, turns: [{ role: 'assistant' as const, kind: 'message' as const, text: 'Save. Response interrupted. 原文', interrupted: true, at: '2026-01-01T00:00:00Z' }] }
  const saved = JSON.parse(JSON.stringify(checkpoint(state, [])))
  const restored = decodeCheckpoint(saved).state
  expect(restored.turns).toEqual(state.turns)
  render(<Conversation state={restored} onSend={() => {}} onStop={() => {}} />)
  await act(async () => { setLanguage('ja'); await languageReady() })
  expect(screen.getByText('Save. Response interrupted. 原文')).toBeTruthy()
  expect(screen.getByText(msg('Response interrupted'))).toBeTruthy()
  expect(restored.turns[0]!.text).toBe(state.turns[0]!.text)
  saved.state.turns[0].interrupted = 'true'
  expect(() => decodeCheckpoint(saved)).toThrow('Saved chat data is not supported')
})

it('translates offline guidance but leaves raw diagnostics that resemble UI labels untouched', async () => {
  const { rerender } = render(<ErrorBox title="Connection" error={new NoSession()} />)
  await act(async () => { setLanguage('fr'); await languageReady() })
  expect(screen.getByText('Aucune session — ouvrez l’URL affichée par jpack-desk au démarrage.')).toBeTruthy()
  expect(new NoSession().message).toBe(NO_SESSION_MESSAGE)
  rerender(<ErrorBox title="Runtime diagnostic" error={new Error('Save')} />)
  expect(screen.getByText('Save')).toBeTruthy()
})
it('localizes nested graph fallback explanations and keeps edge identifiers exact', async () => {
  const read = readGraphDocument('{"nodes":{},"edges":[]}')
  if (read.ok) throw new Error('expected a refused view')
  const notice = walkFallbackReason({ supported: true, drawn: false, served: { meta: { status: 'valid' }, unreadable: read.reason }, error: null })!
  setLanguage('fr'); await languageReady()
  const translated = systemMessage(notice)
  expect(translated).toContain('Vue du graphe indisponible')
  expect(translated).toContain('ne déclare aucun nœud')
  expect(translated).not.toContain('it declares no node')
  expect(edgeCarries({ fact: '/case/Save', evidence: { id: 'Save', onUnresolved: 'withhold' } }, msg)).toBe('/case/Save · preuve Save (withhold si non résolu)')
})
