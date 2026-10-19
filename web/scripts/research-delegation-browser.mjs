import assert from 'node:assert/strict'
import {chromium} from 'playwright-core'
const [origin,setupCode,id]=process.argv.slice(2)
assert(origin&&setupCode&&id,'Use the isolated input review browser fixture')
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',args:['--no-sandbox']})
const page=await browser.newPage({viewport:{width:1500,height:1000},colorScheme:'dark'})
page.setDefaultTimeout(15000)
const errors=[],writes=[];page.on('pageerror',e=>errors.push(e.message))
const answer=(route,value,status=200)=>route.fulfill({status,contentType:'application/json',body:JSON.stringify(value)})
const root=origin+'/api/input-preparations/'+id
const revision='a'.repeat(64),ai='ai-'+'1'.repeat(24)
let saved,recipe={version:1,target:'companies',fields:[{name:'employee_count',description:'Employees',type:'number'}],criteria:[],limits:{records:10,searches:0,pages:20,modelCalls:40},ai:{connection:ai,revision,model:'fixture-model'}},run
await page.route(origin+'/api/ai-connections',route=>answer(route,{version:1,defaultConnection:ai,sha256:revision,path:'fixture',connections:[{id:ai,name:'Fixture API',enabled:true,revision,assistant:{engine:'vercel',thinking:'off',endpoint:{kind:'openai-compatible',url:'https://example.invalid/v1',model:'fixture-model',models:['fixture-model'],tools:[]}}}]}))
await page.route(origin+'/api/assistant/relay/**',route=>{throw Error('No AI request is authorized in this visual fixture')})
await page.route(root,async route=>{
 if(route.request().method()==='PUT'){
  writes.push(route.request().postDataJSON());const body=route.request().postDataJSON();recipe=body.recipe.research;saved={...saved,sha256:'b'.repeat(64),document:{...saved.document,recipe:body.recipe,recipeVersion:2}};return answer(route,saved)
 }
 const response=await route.fetch();const data=await response.json();saved={...data,document:{...data.document,recipe:{...data.document.recipe,research:recipe}}};await answer(route,saved)
})
await page.route(root+'/research',route=>{
 if(route.request().method()==='GET')return answer(route,{items:run?[run]:[]})
 writes.push({review:route.request().postDataJSON()});run={id:'fixture-research',preparationId:id,state:'review',execution:{approach:'delegated',requestedConcurrency:0,concurrency:2,connectionLimit:2},recipeVersion:2,recipe,intent:'Research example companies',createdAt:new Date().toISOString(),updatedAt:new Date().toISOString(),recordCount:0,records:[],sources:[],stopRequested:false};return answer(route,run,201)
})
await page.route(root+'/research/fixture-research/confirm',route=>{
 writes.push({confirm:route.request().postDataJSON()});run={...run,state:'running',recordCount:1,outcomes:{added:1,updated:0,unchanged:0,needsCorrection:0,pending:0}};return answer(route,run)
})
await page.route(root+'/research/fixture-research/activity',route=>{
 const now=new Date().toISOString();return answer(route,{...run,tasks:[{id:'task-a',title:'Company A',objective:'Investigate Company A using retained evidence.',profile:'candidate',state:'running',createdAt:now,startedAt:now,modelRequests:1,sourceRequests:1},{id:'task-b',title:'Company B',objective:'Investigate Company B using retained evidence.',profile:'candidate',state:'complete',createdAt:now,startedAt:now,finishedAt:now,modelRequests:1,sourceRequests:1,result:'Findings saved for review.'}],steps:[{id:'one',taskId:'task-a',kind:'model',state:'started',startedAt:now},{id:'two',taskId:'task-b',kind:'model',state:'completed',startedAt:now,finishedAt:now,result:'records'}]})
})
async function assertHint(){
 const result=await page.waitForFunction(()=>{
  const el=[...document.querySelectorAll('p[id$="-composer-help"]')].find(el=>el.textContent==='Enter to send · Shift+Enter for a new line' && el.getBoundingClientRect().width>0)
  if(!el || getComputedStyle(el).textAlign!=='end')return false
  const range=document.createRange();range.selectNodeContents(el);const rect=range.getBoundingClientRect(),box=el.getBoundingClientRect()
  return {align:getComputedStyle(el).textAlign,delta:Math.abs(rect.right-box.right),overflow:el.scrollWidth>el.clientWidth}
 });const aligned=await result.jsonValue();assert(aligned.delta<2,JSON.stringify(aligned));assert.equal(aligned.overflow,false)
}

try{
 await page.goto(origin);await page.getByRole('button',{name:'Set up sign-in',exact:true}).click();await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode);await page.getByRole('button',{name:'Continue',exact:true}).click();await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 await page.goto(origin);await assertHint();await page.screenshot({path:'/tmp/jps-delegated-main-hint.png'})
 await page.goto(origin+'/dialects/'+id+'?tab=recipe');await assertHint()
 const approach=page.getByRole('combobox',{name:'Research approach',exact:true});assert.match(await approach.innerText(),/Standard/)
 await approach.click();await page.getByRole('option',{name:'Delegated',exact:true}).click()
 const concurrency=page.getByRole('combobox',{name:'Concurrent AI tasks',exact:true});assert.match(await concurrency.innerText(),/Auto/)
 await concurrency.click();await page.getByRole('option',{name:'1',exact:true}).click();assert.match(await concurrency.innerText(),/1/)
 await concurrency.click();await page.getByRole('option',{name:'Auto',exact:true}).click()
 await page.screenshot({path:'/tmp/jps-delegated-recipe.png'})
 await page.getByRole('button',{name:'Save recipe',exact:true}).click();await page.getByRole('button',{name:'Prepare records',exact:true}).click();await page.getByRole('menuitem',{name:'Research with AI…',exact:true}).click()
 const review=page.getByRole('dialog',{name:'Review research',exact:true});await review.getByText('Auto · Up to 2',{exact:true}).waitFor();assert.equal(writes.filter(x=>'confirm'in x).length,0)
 await page.screenshot({path:'/tmp/jps-delegated-review.png'})
 await review.getByRole('button',{name:'Start research',exact:true}).click();await review.waitFor({state:'hidden'});await page.getByRole('button',{name:'View steps',exact:true}).click()
 const activity=page.getByRole('dialog',{name:'Research activity',exact:true});await activity.locator('summary').filter({hasText:'Company A'}).waitFor();assert.equal(await activity.locator('ol[aria-label="Research steps"] > li').count(),2)
 const group=activity.locator('details').filter({has:page.locator('summary').filter({hasText:'Company A'})}).first();assert.equal(await group.locator(':scope > summary > svg').count(),0)
 const spinnerBox=await group.locator(':scope > summary > span').first().boundingBox();assert(spinnerBox.width<=16,'The active indicator must keep the chevron width')
 await activity.locator('summary').filter({hasText:'Company A'}).click();await activity.getByText('AI request',{exact:true}).first().waitFor();await page.screenshot({path:'/tmp/jps-delegated-activity.png'})
 await page.setViewportSize({width:390,height:844});await page.evaluate(()=>document.documentElement.setAttribute('data-theme','light'));const box=await activity.boundingBox();assert(box.x>=0&&box.x+box.width<=390);await page.screenshot({path:'/tmp/jps-delegated-activity-mobile.png'})
 assert.equal(writes.filter(x=>'confirm'in x).length,1);assert.deepEqual(recipe.execution,{approach:'delegated',concurrency:0});assert.deepEqual(errors,[])
 console.log(JSON.stringify({recipeSettings:true,reviewedEffectiveConcurrency:true,explicitStart:true,groupedActivity:true,runningSpinner:true,rightAlignedMainAndPaneHints:true,mobile:true,browserErrors:errors}))
}catch(error){await page.screenshot({path:'/tmp/jps-delegated-browser-failure.png'});console.error((await page.locator('body').innerText()).slice(-6000));throw error}finally{await browser.close()}
