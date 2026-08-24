/**
 * Drives the desk's own MCP client against a running chassis, outside a
 * browser: the same transport and the same SDK Client the page uses.
 *
 * The Go tests prove the relay carries JSON-RPC. This proves the client code
 * on the other end of it speaks to the runtime — the seam between them is
 * where a framing mistake would hide.
 *
 *   node --experimental-strip-types --no-warnings scripts/smoke.ts <url>
 *   npm run smoke -- http://127.0.0.1:8799/?token=…
 */
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { DeskWebSocketTransport } from '../src/mcp/transport.ts'
import type { PackDocument, PackInventory } from '../src/mcp/types.ts'

const target = process.argv[2]
if (!target) {
  console.error('usage: smoke.ts <desk url with ?token=…>')
  process.exit(2)
}

const url = new URL(target)
const token = url.searchParams.get('token')
if (!token) {
  console.error('the URL must carry the session token the chassis printed (?token=…)')
  process.exit(2)
}
const wsURL = `ws://${url.host}/ws?token=${encodeURIComponent(token)}`

function textOf(result: { content?: unknown }): string {
  const blocks = Array.isArray(result.content) ? result.content : []
  return blocks
    .filter((b): b is { type: 'text'; text: string } => (b as { type?: string })?.type === 'text')
    .map((b) => b.text)
    .join('')
}

const client = new Client({ name: 'jpack-desk-smoke', version: '0.1.0' }, { capabilities: {} })

let sawFileChange = false
client.fallbackNotificationHandler = async (notification) => {
  if (notification.method === 'desk/fileChanged') sawFileChange = true
}

await client.connect(new DeskWebSocketTransport(wsURL))

const server = client.getServerVersion()
console.log(`initialize    ok  serverInfo=${server?.name} ${server?.version}`)

const tools = await client.listTools()
console.log(`tools/list    ok  ${tools.tools.length} tools: ${tools.tools.map((t) => t.name).join(', ')}`)

const inventory = JSON.parse(
  textOf(await client.callTool({ name: 'list_packs', arguments: {} }))
) as PackInventory
const packs = inventory.packs ?? []
console.log(`list_packs    ok  status=${inventory.status} packs=${packs.length}`)
for (const pack of packs) {
  console.log(`                  - ${pack.id}  ${pack.packId} v${pack.packVersion}  ${pack.path}`)
}
if (packs.length === 0) {
  console.error('list_packs returned no packs; point the chassis at a project that declares one')
  process.exit(1)
}

const first = packs[0]!
const packResult = await client.callTool({ name: 'get_pack', arguments: { pack_id: first.id } })
const raw = textOf(packResult)
const doc = JSON.parse(raw) as PackDocument
console.log(
  `get_pack      ok  ${raw.length} bytes  title=${JSON.stringify(doc.title)}  ` +
    `rules=${doc.rules?.length ?? 0} outcomes=${doc.outcomes?.length ?? 0} ` +
    `exceptions=${doc.exceptions?.length ?? 0} evidence=${doc.evidenceRequirements?.length ?? 0}`
)

for (const member of ['specVersion', 'id', 'version', 'title', 'decision', 'outcomes', 'rules'] as const) {
  if (doc[member] === undefined) {
    console.error(`the relayed document is missing the required member ${member}`)
    process.exit(1)
  }
}

console.log(`notifications ${sawFileChange ? 'saw desk/fileChanged' : 'none seen (expected: nothing changed)'}`)
console.log('\nOK')
await client.close()
process.exit(0)
