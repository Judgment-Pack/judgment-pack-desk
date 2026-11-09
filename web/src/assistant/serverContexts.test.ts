import { expect,it,vi } from 'vitest'
import { RunRequestError } from './serverRuns/client'
import { ServerContextDocument } from './serverContexts'
function client(){
 let documents=0,panes=0
 return {contextDocument:vi.fn(async()=>({document:String(++documents).padStart(64,'0')})),contextPane:vi.fn(async(document:string)=>({document,pane:String(++panes).padStart(64,'0')})),releaseContext:vi.fn(async()=>{})}
}
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done});return {promise,resolve}}
it('saved navigation shares one document while another actual document has a distinct registration',async()=>{
 const c=client(),doc=new ServerContextDocument(),other=new ServerContextDocument()
 const first=await doc.selector(c),again=await doc.selector(c),next=await other.selector(c)
 expect(first).toEqual(again);expect(next).not.toEqual(first)
 expect(c.contextDocument).toHaveBeenCalledTimes(2)
 expect(c.contextDocument.mock.calls[0]).not.toEqual(c.contextDocument.mock.calls[1])
 expect(c.releaseContext).not.toHaveBeenCalled()
})
it('reconciles lost registration acknowledgement using exactly the original ID',async()=>{
 const c=client();c.contextDocument.mockRejectedValueOnce(new RunRequestError('uncertain'))
 const doc=new ServerContextDocument();await doc.selector(c)
 expect(c.contextDocument).toHaveBeenCalledTimes(2)
 expect(c.contextDocument.mock.calls[0]).toEqual(c.contextDocument.mock.calls[1])
 await doc.selector(c);expect(c.contextDocument).toHaveBeenCalledTimes(2)
})
it('retains uncertain registration instead of silently allocating a replacement',async()=>{
 const c=client();c.contextDocument.mockRejectedValue(new RunRequestError('uncertain'))
 const doc=new ServerContextDocument()
 await expect(doc.selector(c)).rejects.toBeDefined();await expect(doc.selector(c)).rejects.toBeDefined()
 expect(c.contextDocument).toHaveBeenCalledTimes(2)
 expect(c.contextDocument.mock.calls[0]).toEqual(c.contextDocument.mock.calls[1])
})
it('effect replay preserves the same pane, real remount has another registration, and release sends no run action',async()=>{
 const c=client(),doc=new ServerContextDocument(),pane=doc.pane()
 const firstDetach=pane.attach();firstDetach();const detach=pane.attach()
 await Promise.resolve()
 const first=await pane.selector(c,'describe-pack')
 expect(await pane.selector(c,'describe-pack')).toEqual(first)
 expect(c.contextPane).toHaveBeenCalledOnce();expect(c.releaseContext).not.toHaveBeenCalled()
 detach();await Promise.resolve();await Promise.resolve()
 await expect(pane.selector(c,'describe-pack')).rejects.toBeDefined()
 expect(c.releaseContext).toHaveBeenCalledExactlyOnceWith(first)
 const remount=doc.pane();remount.attach()
 expect(await remount.selector(c,'describe-pack')).not.toEqual(first)
 expect(c.contextDocument).toHaveBeenCalledOnce()
 expect(c.contextPane.mock.calls[0]).not.toEqual(c.contextPane.mock.calls[1])
})
it('an actual unmount during registration releases the returned pane without starting or canceling work',async()=>{
 const c=client(),pending=deferred<{document:string;pane:string}>()
 c.contextPane.mockReturnValue(pending.promise)
 const pane=new ServerContextDocument().pane(),detach=pane.attach(),selection=pane.selector(c,'author-pack')
 // Attach the rejection handler before the registration can settle.
 const rejected=expect(selection).rejects.toBeDefined()
 await vi.waitFor(()=>expect(c.contextPane).toHaveBeenCalledOnce())
 detach();await Promise.resolve()
 const value={document:'1'.padStart(64,'0'),pane:'2'.padStart(64,'0')};pending.resolve(value)
 await rejected;await vi.waitFor(()=>expect(c.releaseContext).toHaveBeenCalledExactlyOnceWith(value))
})
it('a pane cannot change purpose and release never allocates an unused document',async()=>{
 const c=client(),doc=new ServerContextDocument(),pane=doc.pane();pane.attach()
 await pane.selector(c,'author-pack')
 await expect(pane.selector(c,'author-graph')).rejects.toBeDefined()
 expect(c.contextPane).toHaveBeenCalledOnce()
 const unused=new ServerContextDocument();unused.release();await expect(unused.selector(c)).rejects.toBeDefined()
 expect(c.contextDocument).toHaveBeenCalledOnce()
})
