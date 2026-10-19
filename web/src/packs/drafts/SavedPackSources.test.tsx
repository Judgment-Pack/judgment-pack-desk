import {cleanup,render,screen,fireEvent} from '@testing-library/react'
import {afterEach,expect,it,vi} from 'vitest'
import {SavedPackSources} from './SavedPackSources'
import {INITIAL_STATE} from '../../research/run'
const fixture=vi.hoisted(()=>({drafts:[] as unknown[],record:undefined as string|undefined}))
vi.mock('../../chat/ChatProvider',()=>({useChats:()=>({ready:true,packDrafts:fixture.drafts})}))
vi.mock('../../files/queries',()=>({useFileListing:()=>({data:{files:fixture.record?[{path:'packs/example.research.json'}]:[]}}),useFileContent:()=>({data:fixture.record?{content:fixture.record}:undefined})}))
afterEach(()=>{cleanup();fixture.drafts=[];fixture.record=undefined})
const inputs={version:1,documents:[],files:[{name:'Policy notes.txt',text:'Source content'}],retrievals:[]}
it('reopens an older finalized pack with its retained draft sources, not another pack’s documents',()=>{
 fixture.drafts=[{id:'draft',documents:[],sourceFiles:inputs.files,checkpoint:{state:INITIAL_STATE,sources:[]},finalized:{id:'example',path:'packs/example.pack.json'}},{documents:[],sourceFiles:[{name:'Other pack.txt',text:'Unrelated'}],finalized:{id:'other',path:'packs/other.pack.json'}}]
 render(<SavedPackSources owner="example" path="packs/example.pack.json" document={{}}/>)
 expect(screen.getByRole('button',{name:'Policy notes.txt'})).toBeTruthy()
 expect(screen.queryByText('Other pack.txt')).toBeNull()
 expect(screen.getByRole('group',{name:'Source controls'}).hasAttribute('data-collection-toolbar')).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'Policy notes.txt'}))
 expect(screen.getByText('Source content')).toBeTruthy()
})
it('reads authoring sources from the saved companion without the original draft',()=>{
 fixture.record=JSON.stringify({authoringSources:inputs})
 render(<SavedPackSources owner="example" path="packs/example.pack.json" document={{}}/>)
 expect(screen.getByRole('button',{name:'Policy notes.txt'})).toBeTruthy()
 expect(screen.getByText('Attached')).toBeTruthy()
 expect(screen.queryByText('Receipt verified')).toBeNull()
})
it('shows exact private Policy origin from finalized provenance without presenting it as a retrieved citation',()=>{
 const source={version:1,policyId:'policy_'+'a'.repeat(48),revisionId:'revision_'+'b'.repeat(48),publicationId:'publication_'+'c'.repeat(48),candidateDigest:'d'.repeat(64),content:{sha256:'d'.repeat(64),bytes:121},title:'Selected Policy',artifactVersion:'1.0.0',narrative:'PRIVATE POLICY NARRATIVE'}
 fixture.drafts=[{id:'draft',documents:[],policySources:[source],checkpoint:{state:INITIAL_STATE,sources:[]},finalized:{id:'example',path:'packs/example.pack.json'}}]
 render(<SavedPackSources owner="example" path="packs/example.pack.json" document={{}}/>)
 expect(screen.getByText('Selected publication')).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'Selected Policy'}))
 expect(screen.getByText(source.revisionId)).toBeTruthy();expect(screen.getByText(source.publicationId)).toBeTruthy()
 expect(screen.queryByText('Receipt verified')).toBeNull();expect(screen.queryByText('Citation traced')).toBeNull()
 expect(screen.queryByText(source.narrative)).toBeNull()
})
