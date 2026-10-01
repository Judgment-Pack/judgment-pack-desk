import {cleanup, fireEvent, render, screen} from '@testing-library/react'
import {MemoryRouter} from 'react-router-dom'
import {Tooltip} from 'radix-ui'
import {afterEach, expect, it, vi} from 'vitest'
import {DraftWorkspace} from './ChatWorkspace'
import {INITIAL_STATE} from '../research/run'
import type {Chat} from '../chat/store'
import type {PackDraft} from '../packs/drafts/model'
import type {ResearchRunBinding} from '../research/useResearchRun'

vi.mock('../chat/ChatProvider', () => ({useChats: () => ({bindings: new Map()})}))
vi.mock('../chat/ChatPanel', () => ({ChatPanel: () => null}))
vi.mock('../packs/drafts/DraftActions', () => ({DraftActions: () => null}))
vi.mock('../packs/folders/FolderBrowser', () => ({ShowFolders: () => null, MovePackButton: () => null}))
vi.mock('../research/run', async original => ({...await original<typeof import('../research/run')>(), canCreateDraft: () => true}))
vi.mock('../research/ui/DraftPanels', () => ({DraftTabs: () => null}))
vi.mock('../packs/test-workspace/store', () => ({useTestStorage: () => ({suite: {cases: [{id: 'independent-one'}, {id: 'unrun-ai'}, {id: 'unrun-manual'}]}})}))
vi.mock('../shell/CreatePackDialog', () => ({CreatePackDialog: ({reviewDraft}: {reviewDraft: {caseCount: number; cases: {total: number}}}) => <div>Independent cases: {reviewDraft.caseCount}; agreement total: {reviewDraft.cases.total}</div>}))
afterEach(cleanup)
it('counts only independently established cases, excluding overlapping and unrun Tests-tab cases', () => {
 const state = {...INITIAL_STATE, status: 'ready', cases: [{id:'independent-one'}, {id:'independent-two'}], candidates:[{digest:'digest',text:'{}',document:{},check:{cases:[{passed:true},{passed:true}]}}]} as unknown as typeof INITIAL_STATE
 render(<Tooltip.Provider><MemoryRouter><DraftWorkspace chat={{id:'chat',mode:'draft'} as Chat} artifact={{id:'draft',title:'Example',generation:1,checkpoint:{state}} as PackDraft} fallback={{state,run:{basisNow:()=>''}} as ResearchRunBinding}/></MemoryRouter></Tooltip.Provider>)
 fireEvent.click(screen.getByRole('button',{name:'Review and finalize'}))
 expect(screen.getByText('Independent cases: 2; agreement total: 2')).toBeTruthy()
})
