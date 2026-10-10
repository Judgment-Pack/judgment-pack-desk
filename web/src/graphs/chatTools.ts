import type { Client } from '@modelcontextprotocol/sdk/client/index.js'
import type { HostTool } from '../assistant/engine'
import { readFile } from '../files/client'
import { graphHostTools } from './author'
import { rehearseGraph } from './GraphRehearsal'
import { graphDraftHref, type GraphDraft } from './drafts'

type Declaration = {path: string; description?: string}
type Configuration = {packs?: Record<string, Declaration>; graphs?: Record<string, Declaration>}
const text = (value: unknown) => {const output = typeof value === 'string' ? value : JSON.stringify(value); if (new TextEncoder().encode(output).length > 1024 * 1024) throw new Error('This decision context exceeds 1 MiB. Read fewer packs at a time.'); return {content: [{type: 'text' as const, text: output}]}}
const schema = (properties: Record<string, unknown>, required: string[] = []) => ({type: 'object', properties, required, additionalProperties: false})
const idSchema = {type: 'string', pattern: '^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$', maxLength: 128}
const configuration = async (signal: AbortSignal) => JSON.parse((await readFile('jpack.json', signal)).content) as Configuration
function declaration(entries: Configuration['packs'], id: unknown): Declaration {
  if (typeof id !== 'string' || !entries || !Object.hasOwn(entries, id) || typeof entries[id]?.path !== 'string') throw new Error('Choose a declared decision id from list_decisions.')
  return entries[id]!
}

/** No write/proposal-confirmation endpoint is exposed to the model. */
export function graphChatTools(options: { client: () => Client | null; chatId: string; addDraft: (draft: GraphDraft) => void }): HostTool[] {
  const readGraphs = new Map<string, {path: string; sha256: string}>()
  return [
    {name: 'list_decisions', description: 'List the project’s declared packs and graphs before composing or running them. Reads declarations; evaluates nothing.', inputSchema: schema({}), execute: async (_args, signal) => {
      const config = await configuration(signal)
      return text({packs: Object.entries(config.packs ?? {}).map(([id, entry]) => ({id, description: entry.description})), graphs: Object.entries(config.graphs ?? {}).map(([id, entry]) => ({id, description: entry.description}))})
    }},
    {name: 'read_decision', description: 'Read a declared pack or graph by type and id. For a graph, also returns its declared pack documents for inspection.', inputSchema: schema({type: {type: 'string', enum: ['pack', 'graph']}, id: idSchema}, ['type', 'id']), execute: async (args, signal) => {
      if (args.type !== 'pack' && args.type !== 'graph') throw new Error('Choose pack or graph.')
      const config = await configuration(signal)
      const file = await readFile(declaration(args.type === 'pack' ? config.packs : config.graphs, args.id).path, signal)
      const packs: {id: string; content: string; sha256: string}[] = []
      if (args.type === 'graph') {
        signal.throwIfAborted()
        readGraphs.set(args.id as string, {path: declaration(config.graphs, args.id).path, sha256: file.sha256})
        const graph = JSON.parse(file.content)
        const ids = [...new Set(Object.values(graph.nodes ?? {}).map(node => (node as {pack: string}).pack))].slice(0, 64)
        for (const id of ids) {
          const pack = await readFile(declaration(config.packs, id).path, signal)
          packs.push({id, content: pack.content, sha256: pack.sha256})
        }
      }
      return text({type: args.type, id: args.id, content: file.content, sha256: file.sha256, packs})
    }},
    {name: 'get_graph_authoring_instructions', description: 'Read the runtime author_graph contract before proposing a graph. Supply the intended relationship and ids of existing packs; never invent pack references.', inputSchema: schema({relationship: {type: 'string', maxLength: 20000}, packs: {type: 'array', items: idSchema, minItems: 1, maxItems: 64}}, ['relationship', 'packs']), execute: async (args, signal) => {
      const client = options.client()
      if (!client) throw new Error('The runtime connection is not ready.')
      if (typeof args.relationship !== 'string' || !Array.isArray(args.packs) || !args.packs.length || args.packs.length > 64) throw new Error('Provide a relationship and up to 64 declared pack ids.')
      const config = await configuration(signal)
      const packs: string[] = []
      for (const id of args.packs) packs.push((await readFile(declaration(config.packs, id).path, signal)).content)
      const result = await client.getPrompt({name: 'author_graph', arguments: {relationship: args.relationship, packs: '[' + packs.join(',') + ']'}}, {signal})
      return text(result.messages.flatMap(message => message.content.type === 'text' ? [message.content.text] : []).join('\n\n') + '\n\nUse propose_graph to retain a graph draft for human review. Do not use a pack proposal fence. Rehearsals run saved graphs only; no rows, lock or project files are written by these tools.')
    }},
    ...graphHostTools(),
    {name: 'propose_graph', description: 'Keep a graph draft in this conversation for the person to inspect, edit and review. Does not save a project file, change configuration or run a decision. For revisions, read the existing graph first.', inputSchema: schema({id: idSchema, content: {type: 'string', maxLength: 1048576}, description: {type: 'string', maxLength: 4096}}, ['id', 'content']), execute: async (args, signal) => {
      if (typeof args.id !== 'string' || !/^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/.test(args.id) || args.id.length > 128 || typeof args.content !== 'string' || new TextEncoder().encode(args.content).length > 1024 * 1024) throw new Error('Provide a graph id and a graph document up to 1 MiB.')
      const doc = JSON.parse(args.content)
      if (!doc || typeof doc !== 'object' || Array.isArray(doc) || doc.formatVersion !== '1' || !doc.nodes || !Array.isArray(doc.edges)) throw new Error('Provide a graph document, not a pack.')
      const config = await configuration(signal)
      const existing = config.graphs && Object.hasOwn(config.graphs, args.id) ? declaration(config.graphs, args.id) : undefined
      const base = existing ? readGraphs.get(args.id) : undefined
      if (existing && (!base || base.path !== existing.path)) throw new Error('Read the current graph with read_decision before proposing a revision.')
      if (existing && (await readFile(existing.path, signal)).sha256 !== base!.sha256) throw new Error('The graph changed since it was read. Read it again and revise the proposal.')
      const draft: GraphDraft = {draftId: crypto.randomUUID(), createdAt: new Date().toISOString(), id: args.id, path: existing?.path ?? `${args.id}.graph.json`, content: args.content, ...(typeof args.description === 'string' ? {description: args.description.slice(0, 4096)} : {}), ...(base ? {baseSha256: base.sha256} : {})}
      signal.throwIfAborted()
      options.addDraft(draft)
      return text({artifactType: 'graph', status: 'draft', draftId: draft.draftId, reviewUrl: graphDraftHref(options.chatId, draft.draftId), message: 'Draft retained for review. No project file was written. Open the draft to validate, review and save.'})
    }},
    {name: 'graph_rehearse', description: 'Rehearse an existing declared graph with the user’s supplied inputs keyed by node id. All nodes run. Does not write an audit decision or execute actions. Ask for missing facts; never invent them. Input and tool result are retained in chat history.', inputSchema: schema({id: idSchema, inputs: {type: 'string', description: 'JSON object keyed by node id; each entry has facts and/or evidence.'}}, ['id', 'inputs']), execute: async (args, signal) => {
      if (typeof args.id !== 'string' || typeof args.inputs !== 'string') throw new Error('A configured graph id and JSON inputs are required.')
      const result = await rehearseGraph(args.id, args.inputs, signal)
      return text(result.raw)
    }}
  ]
}
