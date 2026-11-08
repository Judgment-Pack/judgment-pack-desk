import {cleanup, render, screen} from '@testing-library/react'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach, expect, it, vi} from 'vitest'
import {DraftWorkspace} from './ChatWorkspace'
import {INITIAL_STATE} from '../research/run'
import type {Chat} from '../chat/store'
import type {PackDraft} from '../packs/drafts/model'
import type {ResearchRunBinding} from '../research/types'

const selected = vi.hoisted(() => ({engine: 'codex' as 'codex' | 'vercel'}))
const tabs = vi.hoisted(() => ({props: {} as {onEstablishCases?: unknown; onProposeCorrection?: unknown}}))
vi.mock('../config/DeskConfigProvider', async original => {
  const actual = await original<typeof import('../config/DeskConfigProvider')>()
  const {effectiveConfig} = await import('../config/deskConfig')
  return {...actual, useEffectiveConfig: () => { const base = effectiveConfig(undefined); return {...base, config: {...base.config, assistant: {...base.config.assistant, engine: selected.engine}}} }}
})
vi.mock('../chat/ChatProvider', () => ({useChats: () => ({bindings: new Map()})}))
vi.mock('../chat/ChatPanel', () => ({ChatPanel: () => null}))
vi.mock('../packs/drafts/DraftActions', () => ({DraftActions: () => null}))
vi.mock('../packs/folders/FolderBrowser', () => ({ShowFolders: () => null, MovePackButton: () => null}))
vi.mock('../research/ui/DraftPanels', () => ({DraftTabs: (props: typeof tabs.props) => { tabs.props = props; return null }}))
vi.mock('../packs/test-workspace/store', () => ({useTestStorage: () => ({query: {isSuccess: false}, suite: {cases: []}})}))
afterEach(cleanup)

const notice = 'Writing test cases and suggesting corrections need an API connection in this version. Choose an API connection for this chat to continue; a ChatGPT subscription cannot run them yet.'

// Native-engine creation reviews are refused before they start in v0.14.0
// (Desk #703). The draft says so in words and offers neither review action;
// the API engine keeps both.
it.each([['codex', true], ['vercel', false]] as const)('with the %s engine, creation reviews are offered only off the native engine', (engine, native) => {
  selected.engine = engine
  const state = {...INITIAL_STATE, candidates: [{digest: 'digest', text: '{}', document: {}}]} as unknown as typeof INITIAL_STATE
  const run = {basisNow: () => '', establishCases: vi.fn(), proposeExpectationCorrection: vi.fn()}
  render(<Tooltip.Provider><MemoryRouter><DraftWorkspace chat={{id: 'chat', mode: 'draft', aiConnection: 'one'} as Chat} artifact={{id: 'draft', title: 'Example', generation: 1, checkpoint: {state}} as PackDraft} fallback={{state, run} as unknown as ResearchRunBinding}/></MemoryRouter></Tooltip.Provider>)
  expect(!!screen.queryByText(notice)).toBe(native)
  expect(typeof tabs.props.onEstablishCases).toBe(native ? 'undefined' : 'function')
  expect(typeof tabs.props.onProposeCorrection).toBe(native ? 'undefined' : 'function')
})
