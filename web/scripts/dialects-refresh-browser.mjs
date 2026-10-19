import assert from 'node:assert/strict'
import {chromium} from 'playwright-core'
const [origin,setupCode]=process.argv.slice(2)
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',args:['--no-sandbox']})
const page=await browser.newPage({viewport:{width:1440,height:1000},colorScheme:'dark'})
page.setDefaultTimeout(15000)
const errors=[],models=[]
page.on('pageerror',error=>errors.push(error.message))
page.on('request',req=>{if(req.method()==='POST'&&/\/api\/(codex\/run|assistant\/)/.test(req.url()))models.push(req.url())})
async function api(path,method='GET',body,revision){
 const res=await fetch(origin+path,{method,headers:{Authorization:'Bearer '+setupCode,Origin:origin,'Content-Type':'application/json',...(revision?{'If-Match':revision}:{})},...(body?{body:JSON.stringify(body)}:{})})
 const value=await res.json();assert(res.ok,JSON.stringify(value));return value
}
try{
 await page.goto(origin)
 await page.getByRole('button',{name:'Set up sign-in',exact:true}).click()
 await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode)
 await page.getByRole('button',{name:'Continue',exact:true}).click()
 await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 await page.goto(origin+'/dialects')
 await page.getByText('Prepare data for your decisions',{exact:true}).first().waitFor()
 // A save elsewhere must be discovered without reload, file events or a model.
 const saved=await api('/api/input-preparations','POST',{name:'Public opportunities',recipe:{intent:'Review public notices'}})
 const link=page.getByRole('link',{name:'Public opportunities',exact:true})
 await link.waitFor()
 const updated=await api('/api/input-preparations/'+saved.document.id+'/imports','POST',{kind:'csv',name:'notices.csv',content:'Name,Status\nSupplied notice,Open\n'},saved.sha256)
 await page.waitForFunction(()=>[...document.querySelectorAll('tr')].some(row=>row.textContent.includes('Public opportunities')&&row.textContent.includes('Ready to review')))
 // Reproduce the fresh-load storage failure, then let a read recover itself.
 const detailPath='**/api/input-preparations/'+saved.document.id
 await page.route(detailPath,route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({code:'storage-busy',error:'Dialects are being saved. Try again shortly.'})}))
 await link.click()
 await page.getByRole('button',{name:'Dialect unavailable',exact:false}).waitFor()
 assert.equal(await page.getByText('No records yet',{exact:true}).count(),0)
 await page.unroute(detailPath)
 await page.getByRole('button',{name:'Supplied notice',exact:true}).waitFor()
 await page.getByRole('button',{name:'Dialect unavailable',exact:false}).waitFor({state:'hidden'})
 // Filtering an existing record must never claim the research saved nothing.
 await page.getByRole('searchbox',{name:'Search records',exact:true}).fill('missing')
 await page.getByText('No matching records',{exact:true}).waitFor()
 await page.getByRole('button',{name:'Clear filters',exact:true}).click()
 await page.getByRole('button',{name:'Supplied notice',exact:true}).waitFor()
 await page.getByRole('link',{name:'Dialects',exact:true}).last().click()
 await link.waitFor()
 // A transient refresh error must leave known rows visible.
 await page.route('**/api/input-preparations',route=>route.fulfill({status:503,contentType:'application/json',body:JSON.stringify({error:'Temporarily unavailable'})}))
 await page.getByRole('button',{name:'Dialects unavailable',exact:false}).waitFor()
 assert(await link.isVisible())
 await page.unroute('**/api/input-preparations')
 await page.getByRole('button',{name:'Dialects unavailable',exact:false}).waitFor({state:'hidden'})
 assert(await link.isVisible())
 assert.equal((await api('/api/input-preparations/'+saved.document.id)).sha256,updated.sha256)
 await page.screenshot({path:'/tmp/dialects-refresh-browser.png'})
 assert.deepEqual(errors,[]);assert.deepEqual(models,[])
 console.log(JSON.stringify({externalSaveAppears:true,recordCountRefreshes:true,filterDoesNotClaimDataLoss:true,readFailurePreservesRows:true,automaticRecovery:true,initialDetailFailureRecovers:true,noModelRequests:true}))
}catch(error){await page.screenshot({path:'/tmp/dialects-refresh-failure.png'});console.error((await page.locator('body').innerText()).slice(-3000));throw error}finally{await browser.close()}
