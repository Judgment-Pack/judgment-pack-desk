import { expect, it } from 'vitest'
import { workItems, readResponseHistory } from './responseHistory'
import { validLinkStep } from './linkStep'
const doc=(n:string)=>({documentId:`12345678-1234-1234-1234-12345678901${n}`,digest:'sha256:'+n.repeat(64),pages:[1]})
it('binds out-of-order link results to exact URLs, documents and read windows, and saves them',()=>{
 const rows=workItems([
  {type:'tool_call',callId:'a',name:'read_link',args:{url:'https://a.example/#section'}},
  {type:'tool_call',callId:'b',name:'read_link',args:{url:'https://b.example/'}},
  {type:'tool_result',callId:'b',name:'read_link',isError:false,text:'raw page',structured:{...doc('2'),link:'https://b.example/'}},
  {type:'tool_result',callId:'a',name:'read_link',isError:false,text:'raw page',structured:{...doc('1'),link:'https://a.example/#section',pages:[2,3]}}
 ],false)
 expect(rows[0]?.link).toEqual({url:'https://a.example/#section',...doc('1'),pages:[2,3]})
 expect(rows[1]?.link).toEqual({url:'https://b.example/',...doc('2')})
 const saved={id:'response',documents:[],websites:[],sourceIds:[],work:{items:rows,notices:[]}}
 expect(readResponseHistory(JSON.parse(JSON.stringify([saved])))[0]?.work.items).toEqual(rows)
 expect(JSON.stringify(rows)).not.toContain('raw page')
})
it('retains a requested URL on failure, without authorizing a document from an error',()=>{
 const rows=workItems([{type:'tool_call',callId:'a',name:'read_link',args:{url:'https://example.org/'}},{type:'tool_result',callId:'a',name:'read_link',isError:true,text:'private error',structured:{...doc('1'),link:'https://example.org/'}}],false)
 expect(rows[0]?.link).toEqual({url:'https://example.org/'})
})
it('rejects unsafe URLs, malformed references and invalid read windows',()=>{
 for(const value of [{url:'javascript:alert(1)'},{url:'https://user:secret@example.org/'},{url:'https://example.org/',...doc('1'),pages:[0]},{url:'https://example.org/',...doc('1'),pages:[1,1]},{url:'https://example.org/',documentId:doc('1').documentId}])expect(validLinkStep(value)).toBe(false)
})
it('saves a typed origin refusal without raw provider text or evidence',()=>{
 const items=workItems([{type:'tool_call',callId:'redirect',name:'read_link',args:{url:'https://example.org/'}},{type:'tool_result',callId:'redirect',name:'read_link',isError:true,text:'private-error-secret',structured:{linkFailure:'web-redirect-origin',...doc('1'),link:'https://example.org/'}}],false)
 const saved={id:'response',documents:[],websites:[],sourceIds:[],work:{items,notices:[]}}
 const loaded=readResponseHistory(JSON.parse(JSON.stringify([saved])))[0]!.work.items[0]!
 expect(loaded.linkFailure).toBe('web-redirect-origin');expect(loaded.link?.documentId).toBeUndefined()
 expect(JSON.stringify(loaded)).not.toContain('private-error-secret')
 for(const patch of [{linkFailure:'private-error-secret'},{status:'complete'},{name:'search_sources'}])expect(()=>readResponseHistory([{...saved,work:{items:[{...loaded,...patch}],notices:[]}}])).toThrow()
})
