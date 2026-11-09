import {describe, expect, it} from 'vitest'
import {readExecution} from '@judgment-pack/harness/execution/state'
import {checkpoint, decodeCheckpoint} from '../chat/checkpoint'
import {INITIAL_STATE} from '../research/run'

describe('Agent v0.6.0 retained checkpoint compatibility', () => {
  it('keeps approval framing and loaded-skill metadata through published and saved-chat readers', () => {
    const marker = '0123456789abcdef0123456789abcdef'
    const skills = [{id:'fixture', digest:'sha256:' + 'a'.repeat(64), bytes:25, loadedAt:'2026-10-20T00:00:00Z'}]
    const sources = [{source:'fixture', kind:'custom', calls:0, refused:0, proposed:0, bytes:0, milliseconds:0, skills:1}]
    const raw = {
      version:1, id:'release-fixture', requestKey:'b'.repeat(64), phase:'work', status:'paused', unit:'model-requests',
      used:1, limit:20, grants:[], plan:[], explanation:'', operations:[], context:{scope:'fixture', entries:[]},
      approval:{callId:'call-one', name:'write_file', tool:'write_file', source:{kind:'custom', id:'fixture', trust:'person'}, args:{Fact:false, fact:null}, state:'asked', marker},
      skills, sources
    }
    const execution = readExecution(raw)
    expect(execution).toBeDefined()
    const saved = decodeCheckpoint(JSON.parse(JSON.stringify(checkpoint({...INITIAL_STATE, execution}, [])))).state.execution!
    expect(saved.approval?.marker).toBe(marker)
    expect(saved.approval?.args).toEqual({Fact:false, fact:null})
    expect(saved.skills).toEqual(skills)
    expect(saved.sources).toEqual(sources)
    expect(saved).toMatchObject({unit:'model-requests', used:1, limit:20, grants:[], operations:[]})
    // These are retained records, not host skill declarations or tool grants.
    expect(saved).not.toHaveProperty('hostTools')
  })
})
