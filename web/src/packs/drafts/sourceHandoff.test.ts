import {expect,it} from 'vitest'
import {INITIAL_STATE} from '../../research/run'
import {readSourceHandoff,restoreSourceHandoff,sourceHandoff} from './sourceHandoff'

const document={id:'12345678-1234-1234-1234-000000000001',name:'Requirements.pdf',text:'',document:{id:'12345678-1234-1234-1234-000000000001',digest:'sha256:'+'a'.repeat(64),pages:[1,2],allowPartial:false}}
it('retains exact document revisions, text files and failed source requests in the saved companion',()=>{
 const draft={documents:[document],sourceFiles:[{name:'Notes.txt',text:'Independent requirements'}],checkpoint:{state:INITIAL_STATE,sources:[{session:'session-one',kind:'page' as const,request:{source:'web',dialect:'jps-web-research' ,url:'https://example.org'},requestedAt:'2026-10-13T12:00:00Z',text:null,failure:'Could not read',quotes:[]}]}}
 const serialized=JSON.parse(JSON.stringify(sourceHandoff(draft)))
 const saved=readSourceHandoff(serialized)
 expect(saved.documents).toEqual([document]);expect(saved.files).toEqual(draft.sourceFiles)
 const restored=restoreSourceHandoff(saved.retrievals)
 expect(restored).toHaveLength(1);expect(restored[0]?.failure).toBe('Could not read');expect(restored[0]?.verification.state).not.toBe('verified')
})
it('rejects malformed retained inputs without claiming empty sources',()=>{
 expect(()=>readSourceHandoff({version:1,documents:[{...document,document:{...document.document,digest:'invalid'}}],files:[],retrievals:[]})).toThrow()
 expect(()=>readSourceHandoff({version:1,documents:[],files:null,retrievals:[]})).toThrow()
})
