import { expect,it } from 'vitest'
import { readResourcePage, readResourceStatus } from './resourceProtocol'
const page=()=>({selectionContext:'epoch',items:[{id:'a',title:'Policy',url:'',sizeBytes:12}],more:true,nextPageToken:'page-2'})
it('keeps cursor and unavailable metadata without pretending every listed object can be read',()=>{
 const raw=page();Object.assign(raw.items[0]!,{unavailableReason:'archived'})
 expect(readResourcePage(raw).items[0]?.unavailableReason).toBe('archived')
 expect(readResourcePage(raw).nextPageToken).toBe('page-2')
})
it.each(['duplicate','cursor','context','url','size','title','count','bytes'] as const)('rejects malformed resource page: %s',kind=>{
 const raw=page()
 if(kind==='duplicate')raw.items.push(raw.items[0]!)
 if(kind==='cursor')raw.nextPageToken=''
 if(kind==='context')raw.selectionContext=''
 if(kind==='url')raw.items[0]!.url='https://example.com/file?token=secret'
 if(kind==='size')raw.items[0]!.sizeBytes=-1
 if(kind==='title')raw.items[0]!.title='x'.repeat(1025)
 if(kind==='count')raw.items=Array.from({length:51},(_,i)=>({id:String(i),title:'Policy',url:'',sizeBytes:12}))
 if(kind==='bytes')raw.items=Array.from({length:30},(_,i)=>({id:String(i)+'x'.repeat(2048),title:'Policy',url:'',sizeBytes:12}))
 expect(()=>readResourcePage(raw)).toThrow()
})
it('validates public scope and limits without accepting a larger contract implicitly',()=>{
 const raw={version:1,provider:'fixture-files',state:'connected',resource:{id:'bucket-a',name:'Policies / Team A'},maxFiles:4,maxFileBytes:4<<20}
 expect(readResourceStatus(raw,'fixture-files').resource?.name).toBe('Policies / Team A')
 expect(()=>readResourceStatus({...raw,maxFiles:5},'fixture-files')).toThrow()
 expect(()=>readResourceStatus({...raw,maxFileBytes:16<<20},'fixture-files')).toThrow()
 expect(()=>readResourceStatus(raw,'other-provider')).toThrow()
 expect(()=>readResourceStatus({...raw,resource:{id:'bucket-a',name:{secret:'no'}}},'fixture-files')).toThrow()
})
