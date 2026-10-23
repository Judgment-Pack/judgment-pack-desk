// Isolated invariant probe. Only transport dependencies are stubbed; run client,
// envelope decoding and stream reconciliation are the production modules.
import assert from 'node:assert/strict'
import pending from '../web/src/assistant/serverRuns/fixtures/pending.json'
import terminal from '../web/src/assistant/serverRuns/fixtures/terminal.json'
import { decodeSnapshot } from '../web/src/assistant/serverRuns/decode.ts'
import { RunObservations } from '../web/src/assistant/serverRuns/stream.ts'
import { serverRunClient } from '../web/src/assistant/serverRuns/client.ts'
const ref={id:pending.id,owner:pending.owner,requestId:pending.requestId}
assert.throws(()=>decodeSnapshot({...pending,owner:{...pending.owner,revision:'b'.repeat(64)}},ref),'source revision must remain exact')
assert.throws(()=>decodeSnapshot({...pending,checkpoint:{}},ref),'checkpoint must use canonical reader')
assert.throws(()=>decodeSnapshot({...pending,events:[{id:'e',event:{type:'invented'}}],cursor:'1:1'},ref),'unknown union discriminator')
const observations=new RunObservations(ref,decodeSnapshot(pending,ref))
observations.accept({kind:'event',cursor:'3:1',data:terminal.events[0]})
observations.accept({kind:'event',cursor:'3:2',data:terminal.events[1]})
assert.equal(observations.accept({kind:'snapshot',cursor:'3:2',data:terminal})?.kind,'snapshot','same cursor must not suppress snapshot')
let posts=0
const stub=async()=>{posts++;if(posts===1)throw Error('uncertain delivery');return new Response(JSON.stringify(pending),{headers:{'Content-Type':'application/json'}})}
globalThis.fetch=stub
const client=await serverRunClient()
await assert.rejects(client.start({admissionId:'admission-one',purpose:'chat',ownerId:ref.owner.id,revision:ref.owner.revision,requestId:ref.requestId}),error=>error.delivery==='uncertain','uncertain POST must not replay')
assert.equal(posts,1,'uncertain POST must never automatically repeat')
