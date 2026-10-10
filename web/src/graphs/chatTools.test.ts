import { beforeEach, expect, it, vi } from 'vitest'
import type { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { graphChatTools } from './chatTools'
import { readFile, deskFetch } from '../files/client'
import { validGraphDraft } from './drafts'

vi.mock('../files/client', async original => ({...await original(), readFile: vi.fn(), deskFetch: vi.fn()}))
const graph = '{"formatVersion":"1","id":"flow","version":"1.0.0","nodes":{"a":{"pack":"alpha"}},"edges":[],"result":"a"}'
const signal = () => new AbortController().signal
let config: {packs: Record<string, {path: string}>; graphs: Record<string, {path: string}>}
let hash: string
beforeEach(() => {
  vi.resetAllMocks(); hash = 'a'.repeat(64); config = {packs: {alpha: {path:'alpha.json'}}, graphs: {flow: {path: 'flow.json'}}}
  vi.mocked(readFile).mockImplementation(async path => ({path, content: path === 'jpack.json' ? JSON.stringify(config) : path === 'flow.json' ? graph : '{"id":"alpha","version":"1.0.0"}', bytes: 100, sha256: hash}))
})
function tools() {
  const addDraft = vi.fn(), getPrompt = vi.fn(async () => ({messages: [{content: {type:'text', text: 'runtime instructions'}}]}))
  const all = graphChatTools({chatId:'chat-1', addDraft, client: () => ({getPrompt}) as unknown as Client})
  return {addDraft, getPrompt, all, call: (name: string, args: Record<string, unknown>, abort = signal()) => all.find(t => t.name === name)!.execute(args, abort)}
}
it('proposes a typed graph draft without writing project files or invoking evaluation', async () => {
  const t = tools()
  const result = await t.call('propose_graph', {id:'new-flow', content: graph})
  expect(t.addDraft).toHaveBeenCalledOnce()
  const draft = t.addDraft.mock.calls[0][0]
  expect(validGraphDraft(draft)).toBe(true)
  expect(draft).toMatchObject({id:'new-flow', path:'new-flow.graph.json', content:graph})
  expect(JSON.stringify(result)).toContain('artifactType')
  expect(JSON.stringify(result)).toContain('/graphs?chat=chat-1')
  expect(deskFetch).not.toHaveBeenCalled()
  expect(t.all.map(t => t.name)).not.toContain('graph_write')
})
it('requires an existing graph to be read and refuses to rebase a stale proposal', async () => {
  const t = tools()
  await expect(t.call('propose_graph', {id:'flow', content:graph})).rejects.toThrow('Read the current graph')
  await t.call('read_decision', {type:'graph', id:'flow'})
  hash = 'b'.repeat(64)
  await expect(t.call('propose_graph', {id:'flow', content:graph})).rejects.toThrow('changed since it was read')
  expect(t.addDraft).not.toHaveBeenCalled()
  await t.call('read_decision', {type:'graph', id:'flow'})
  await t.call('propose_graph', {id:'flow', content:graph})
  expect(t.addDraft.mock.calls[0][0].baseSha256).toBe(hash)
})
it('refuses undeclared reads and does not return private project settings in inventory', async () => {
  const t = tools()
  await expect(t.call('read_decision', {type:'pack', id:'../secret'})).rejects.toThrow('declared')
  await expect(t.call('read_decision', {type:'pack', id:'toString'})).rejects.toThrow('declared')
  const result = await t.call('list_decisions', {})
  expect(JSON.stringify(result)).not.toContain('path')
  expect(deskFetch).not.toHaveBeenCalled()
})
it('fetches the runtime graph contract with the selected real pack bytes', async () => {
  const t = tools()
  const result = await t.call('get_graph_authoring_instructions', {relationship:'Alpha decides.', packs:['alpha']})
  expect(t.getPrompt).toHaveBeenCalledWith({name:'author_graph', arguments:{relationship:'Alpha decides.', packs:'[{"id":"alpha","version":"1.0.0"}]'}}, {signal:expect.any(AbortSignal)})
  expect(JSON.stringify(result)).toContain('runtime instructions')
  await expect(t.call('get_graph_authoring_instructions', {relationship:'Missing', packs:['unknown']})).rejects.toThrow('declared')
})
it('runs only the saved graph rehearsal and preserves exact runtime result bytes', async () => {
  const t = tools(), raw = '{ "command":"experimental graph evaluate", "status":"evaluated", "rehearsal":true,"number":9007199254740993 }'
  vi.mocked(deskFetch).mockResolvedValueOnce(new Response('{"answer":'+raw+'}'))
  const result = await t.call('graph_rehearse', {id:'flow',inputs:'{"a":{"facts":{"value":1.0}}}'})
  expect(result.content).toEqual([{type:'text',text:raw}])
  expect(deskFetch).toHaveBeenCalledWith('/api/graphs/evaluate?id=flow',expect.objectContaining({method:'POST',body:'{"a":{"facts":{"value":1.0}}}'}))
})
it('does not retain an aborted proposal or a pack disguised as a graph', async () => {
  const t = tools(), controller = new AbortController(); controller.abort()
  await expect(t.call('propose_graph', {id:'new-flow',content:graph}, controller.signal)).rejects.toThrow()
  await expect(t.call('propose_graph', {id:'new-flow',content:'{"specVersion":"0.2.0-draft"}'})).rejects.toThrow('graph document')
  expect(t.addDraft).not.toHaveBeenCalled()
})
