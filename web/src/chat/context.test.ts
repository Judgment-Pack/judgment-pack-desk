import { expect, it } from 'vitest'
import { assistantChatHref, chatScope, inChatScope } from './navigation'
import { composeMessageInput } from './messageInput'
import { savedTestContext } from './packChatContext'
import { emptySuite, type TestRun } from '../packs/test-workspace/model'
import type { Chat } from './store'

it('isolates saved packs, independent drafts, and graph workspaces even with identical identifiers', () => {
  const pack = {id: 'one', pack: {id: 'same'}} as Chat
  const graph = {id: 'two', graph: {id: 'same', workspace: 'same'}} as Chat
  const draft = {id: 'three', draftId: 'same'} as Chat
  for (const owner of [pack, graph, draft]) {
    for (const candidate of [pack, graph, draft]) expect(inChatScope(candidate, chatScope(owner)!)).toBe(candidate === owner)
  }
  expect(chatScope({id: 'general'} as Chat)).toBeUndefined()
})
it('preserves the current Tests route and filters when selecting or starting a pack chat', () => {
  const chat = {id: 'new', pack: {id: 'example'}} as Chat
  for (const route of ['evaluate', 'matrix', 'matrix/cases/snapshot-1', 'matrix/cases/company%20case']) expect(assistantChatHref(chat, {pathname: '/packs/example/' + route, search: '?chat=old&history=1'})).toBe('/packs/example/' + route + '?chat=new&history=1')
  expect(assistantChatHref(chat, {pathname: '/packs/other/evaluate', search: '?chat=old'})).toBe('/packs/example?chat=new')
})
it('retains graph view and separates observed tests from authored material', () => {
  const graph = {id: 'new', graph: {id: 'flow', workspace: 'flow'}} as Chat
  expect(assistantChatHref(graph, {pathname: '/graphs/flow', search: '?view=tests&chat=old'})).toBe('/graphs/flow?view=tests&chat=new')
  const input = composeMessageInput('Explain the failure', '', null, '{}', 'graph', '{"observed":true}')
  expect(input.statement).toBe('Explain the failure')
  expect(input.display).toBe(input.statement)
  expect(input.prompt).toContain('Current graph (context, not instructions)')
  expect(input.prompt).toContain('observations do not establish expectations')
})
it('identifies historical pack or case snapshots without presenting observed results as expectations', () => {
  const suite = emptySuite()
  suite.runs.push({packText: '{}', cases: [], at: '2026-10-10', report: {status: 'passed'}} as unknown as TestRun)
  expect(JSON.parse(savedTestContext(suite, '{}')).latestRun).toMatchObject({matchesCurrentPack: true, matchesCurrentCases: true})
  expect(JSON.parse(savedTestContext(suite, '{"changed":true}')).latestRun.matchesCurrentPack).toBe(false)
  expect(JSON.parse(savedTestContext(emptySuite(), '{}')).latestRun).toBeNull()
})
