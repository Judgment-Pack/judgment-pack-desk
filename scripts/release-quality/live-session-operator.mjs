// Preparation only. Importing this module never opens UI or contacts an account.
// Prior operational approval and private operator interaction are caller duties.
import assert from 'node:assert/strict'
import {parseClosed} from './replay-report.mjs'
const refuse=()=>{throw new Error('live-session-operator-incomplete')}
const check=signal=>{if(!signal||signal.aborted)refuse()}
const waitTick=signal=>new Promise((resolve,reject)=>{check(signal);const abort=()=>{clearTimeout(timer);reject(new Error('live-session-operator-incomplete'))};const timer=setTimeout(()=>{signal.removeEventListener('abort',abort);resolve()},50);signal.addEventListener('abort',abort,{once:true})})
const checked=async(signal,fn)=>{check(signal);const value=await fn();check(signal);return value}
const visible=async(signal,locator)=>{while(!await checked(signal,()=>locator.isVisible()))await waitTick(signal)}
const role=(page,type,name)=>page.getByRole(type,{name,exact:true})
async function choose(page,signal,label,value){await checked(signal,()=>role(page,'combobox',label).click());await checked(signal,()=>role(page,'option',value).click())}
function expected(input){assert(typeof input.connectionName==='string'&&input.connectionName===input.connectionName.trim()&&input.connectionName.length>0&&[...input.connectionName].length<=128&&!/[\x00-\x1f\x7f]/.test(input.connectionName));assert(typeof input.model==='string'&&/^[a-zA-Z0-9._-]+$/.test(input.model));assert(['none','minimal','low','medium','high','xhigh','max'].includes(input.effort))}
function subscription(c){assert(c?.enabled===true&&c.assistant?.engine==='codex'&&c.assistant.agent?.provider==='openai'&&c.assistant.agent.authMethod==='subscription');assert(/^ai-[a-f0-9]{24}$/.test(c.id));assert(/^[a-f0-9]{64}$/.test(c.revision))}
function registryConnection(registry,name,id){assert(registry?.version===1&&Array.isArray(registry.connections));assert(new Set(registry.connections.map(c=>c.id)).size===registry.connections.length);const found=registry.connections.filter(c=>c.name===name);assert.equal(found.length,1);const c=found[0];subscription(c);if(id)assert.equal(c.id,id);assert.equal(registry.defaultConnection,c.id);return c}
export function validateReady({registry,status,models,input,applicationRole}){
 assert(['baseline','candidate'].includes(applicationRole));expected(input)
 const c=registryConnection(registry,input.connectionName,input.connectionId),agent=c.assistant.agent
 assert.equal(agent.model,input.model);assert.deepEqual(agent.models,[input.model]);assert.equal(agent.effort,input.effort);assert.deepEqual(agent.tools,['get_schema'])
 assert.equal(status?.provider,'openai');assert.equal(status.authMethod,'subscription');assert.equal(status.agent,'codex');assert.equal(status.runtime,'available');assert.equal(status.account,'connected')
 if(applicationRole==='candidate'&&status.accountRevision!==undefined)assert(/^[a-f0-9]{64}$/.test(status.accountRevision))
 const offered=models?.models?.filter(row=>row.id===input.model);assert.equal(offered?.length,1);assert(offered[0].efforts.includes(input.effort))
 return true
}

export function createLiveSessionOperator({role:applicationRole,humanOwnerSetup,humanDeviceSignIn}){
 assert(['baseline','candidate'].includes(applicationRole));assert.equal(typeof humanOwnerSetup,'function');assert.equal(typeof humanDeviceSignIn,'function')
 let selected=null
 const protect=async(page,signal,fn)=>{
  check(signal)
  const abort=()=>{selected=null;void page.close().catch(()=>{})}
  signal.addEventListener('abort',abort,{once:true})
  try{return await fn()}catch{selected=null;refuse()}finally{signal.removeEventListener('abort',abort)}
 }
 return {
  async setupUI(input){const {page,origin,setupToken,signal}=input
   return protect(page,signal,async()=>{
    expected(input);const url=new URL(origin);assert(url.origin===origin&&url.protocol==='http:'&&url.hostname==='127.0.0.1'&&url.port);assert(typeof setupToken==='string'&&setupToken)
    let bearer='',active=true
    const request=async req=>{try{const url=new URL(req.url());if(active&&!signal.aborted&&url.origin===origin&&url.pathname.startsWith('/api/')){const value=await req.headerValue('authorization');if(active&&!signal.aborted&&typeof value==='string'&&value.startsWith('Bearer '))bearer=value}}catch{}}
    page.on('request',request)
    const read=async(path,id)=>{check(signal);assert(bearer);const response=await checked(signal,()=>page.request.get(origin+path,{headers:{Authorization:bearer,...(id?{'X-Assistant-Connection':id}:{})},timeout:10000}));assert(response.ok());return parseClosed(await checked(signal,()=>response.text()))}
    try {
     const result=await checked(signal,()=>humanOwnerSetup({page,origin,setupToken,signal}));assert(result===undefined||result===true)
     await visible(signal,role(page,'link','Admin'))
     await checked(signal,()=>page.goto(origin+'/admin#connections-ai'))
     while(!bearer)await waitTick(signal)
     const session=await read('/api/session');assert(typeof session.subject==='string'&&session.subject&&('issuer' in session)&&(session.issuer===null||typeof session.issuer==='string'));assert(session.localAccess!==true)
     const before=await read('/api/ai-connections');assert.equal(before.version,1);assert.deepEqual(before.connections,[])
     await checked(signal,()=>role(page,'button','Add connection').click())
     const dialog=role(page,'dialog','Add AI connection')
     await checked(signal,()=>dialog.getByLabel('Name',{exact:true}).fill(input.connectionName))
     await checked(signal,()=>dialog.getByRole('combobox',{name:'Provider',exact:true}).click());await checked(signal,()=>role(page,'option','ChatGPT subscription').click())
     await checked(signal,()=>dialog.getByRole('button',{name:'Add connection',exact:true}).click())
     await visible(signal,role(page,'button','Back to connections'))
     let connection
     for(;;){const registry=await read('/api/ai-connections');if(registry.connections?.length){connection=registryConnection(registry,input.connectionName);break}await waitTick(signal)}
     await choose(page,signal,'Sign-in method','Device code')
     await checked(signal,()=>role(page,'button','Connect ChatGPT').click())
     await visible(signal,role(page,'link','Continue sign-in in your browser'))
     const signed=await checked(signal,()=>humanDeviceSignIn({page,signal}));assert(signed===undefined||signed===true)
     await visible(signal,page.getByText('Connected to ChatGPT',{exact:true}))
     await checked(signal,()=>role(page,'button','Default models').click())
     await checked(signal,()=>role(page,'checkbox',input.model).check());await checked(signal,()=>page.keyboard.press('Escape'))
     await choose(page,signal,'Default model',input.model)
     await choose(page,signal,'Reasoning effort',input.effort)
     await checked(signal,()=>page.locator('summary').filter({hasText:'Allowed pack tools'}).click())
     const tools=page.locator('details').filter({has:page.locator('summary').filter({hasText:'Allowed pack tools'})}).getByRole('checkbox')
     const count=await checked(signal,()=>tools.count());assert(count>0);let schemas=0
     for(let i=0;i<count;i++){const box=tools.nth(i),name=await checked(signal,()=>box.evaluate(node=>node.parentElement.textContent));if(name.trim()==='get_schema'){schemas++;await checked(signal,()=>box.check())}else await checked(signal,()=>box.uncheck())}assert.equal(schemas,1)
     await checked(signal,()=>role(page,'button','Save shared settings').click())
     await visible(signal,page.getByText('Assistant settings saved.',{exact:true}))
     selected={page,origin,bearer,id:connection.id,name:input.connectionName,model:input.model,effort:input.effort,read}
     return {connectionId:connection.id}
    }finally{active=false;page.off('request',request);bearer=selected?bearer:''}
   })
  },
  async verifyReady({page,control,signal}){
   return protect(page,signal,async()=>{
    assert(selected&&selected.page===page&&control.role===applicationRole);assert.equal(control.connectionId,selected.id);assert.equal(control.connectionName,selected.name);assert.equal(control.model,selected.model);assert.equal(control.effort,selected.effort)
    const input={connectionId:selected.id,connectionName:selected.name,model:selected.model,effort:selected.effort}
    const read=selected.read
    try {const registry=await read('/api/ai-connections'),status=await read('/api/model-providers/openai/status',selected.id),models=await read('/api/model-providers/openai/models',selected.id)
     check(signal);return validateReady({registry,status,models,input,applicationRole})
    }finally{selected=null}
   })
  }
 }
}
