import assert from 'node:assert/strict'
import {chromium} from 'playwright-core'
const [origin,setupCode,id]=process.argv.slice(2)
assert(origin&&setupCode&&id,'Use the disposable TestInputRecordReviewBrowser fixture')
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',args:['--no-sandbox']})
const page=await browser.newPage({viewport:{width:1440,height:900},colorScheme:'dark'})
page.setDefaultTimeout(15000)
const errors=[],writes=[];page.on('pageerror',e=>errors.push(e.message))
const root=origin+'/api/input-preparations/'+id
const recipe={version:1,target:'companies',fields:[{name:'employee_count',description:'Employees',type:'number'}],criteria:[],limits:{records:10,searches:10,pages:20,modelCalls:40},ai:{connection:'fixture-ai',revision:'fixture-revision',model:'fixture-model'}}
let saved,run
const answer=(route,value,status=200)=>route.fulfill({status,contentType:'application/json',body:JSON.stringify(value)})
await page.route(root,async route=>{
 const response=await route.fetch();saved=await response.json();saved.document.recipe.research=recipe
 await answer(route,saved)
})
await page.route(root+'/research',async route=>{
 if(route.request().method()==='GET')return answer(route,{items:run?[run]:[]})
 const body=route.request().postDataJSON();writes.push(body)
 assert.deepEqual(body,{recordIds:['row-1','row-3']})
 run={id:'reviewed-selection',preparationId:id,state:'review',reprocess:true,recipeVersion:1,recipe:{...recipe,limits:{...recipe.limits,records:2,searches:0,pages:0}},intent:'Review example companies',createdAt:new Date().toISOString(),updatedAt:new Date().toISOString(),recordCount:0,records:[],sources:[{id:'saved-source'}],selection:body.recordIds.map(recordId=>({recordId,entity:saved.document.records.find(r=>r.id===recordId).research.entity})),outcomes:{added:0,updated:0,unchanged:0,needsCorrection:0,pending:0},stopRequested:false}
 await answer(route,run,201)
})
await page.route(root+'/research/reviewed-selection/confirm',route=>{
 writes.push({confirm:route.request().postDataJSON()});run={...run,state:'completed',recordCount:2,outcomes:{added:0,updated:1,unchanged:1,needsCorrection:0,pending:0}}
 return answer(route,run)
})
try{
 await page.goto(origin);await page.getByRole('button',{name:'Set up sign-in',exact:true}).click();await page.getByLabel('Owner setup code',{exact:true}).fill(setupCode);await page.getByRole('button',{name:'Continue',exact:true}).click();await page.getByRole('button',{name:'Set password',exact:true}).waitFor()
 await page.goto(origin+'/dialects/'+id);await page.getByRole('button',{name:'Example company 7',exact:true}).waitFor()
 await page.locator('.desk-pane-head').getByRole('button',{name:/^Collapse /}).click()
 await page.getByRole('checkbox',{name:'Select Example company 1',exact:true}).check();await page.getByRole('checkbox',{name:'Select Example company 3',exact:true}).check()
 await page.getByRole('button',{name:'2 selected',exact:true}).click()
 await page.getByRole('menuitem',{name:'Reprocess from saved sources…',exact:true}).waitFor()
 await page.screenshot({path:'/tmp/jps-reprocess-selection.png'})
 await page.getByRole('menuitem',{name:'Reprocess from saved sources…',exact:true}).click()
 const dialog=page.getByRole('dialog',{name:'Reprocess 2 records',exact:true})
 await dialog.getByText('Current saved recipe · Version 1',{exact:true}).waitFor()
 assert.equal(writes.length,1,'Review must not start processing')
 await dialog.getByText('Selected records · 2',{exact:true}).click()
 await dialog.getByText('Example company 1',{exact:true}).waitFor();await dialog.getByText('Example company 3',{exact:true}).waitFor()
 assert.equal(await dialog.getByText('Example company 2',{exact:true}).count(),0)
 await page.screenshot({path:'/tmp/jps-reprocess-review.png'})
 await page.setViewportSize({width:390,height:844});await page.evaluate(()=>document.documentElement.setAttribute('data-theme','light'))
 const bounds=await dialog.boundingBox();assert(bounds.x>=0&&bounds.x+bounds.width<=390)
 await page.screenshot({path:'/tmp/jps-reprocess-mobile.png'})
 const start=dialog.getByRole('button',{name:'Reprocess 2 records',exact:true});await start.focus();await page.keyboard.press('Enter');await dialog.waitFor({state:'hidden'})
 await page.getByLabel('Research status',{exact:true}).getByText('Reprocessing complete · 1 updated · 1 unchanged',{exact:true}).waitFor()
 assert.deepEqual(writes,[{recordIds:['row-1','row-3']},{confirm:{}}])
 assert.equal(await page.getByRole('button',{name:/Continue from saved|Continue research/}).count(),0)
 assert.deepEqual(errors,[])
 console.log(JSON.stringify({selectionScoped:true,explicitConfirmation:true,keyboard:true,mobile:true,noDuplicateProgressAction:true,browserErrors:errors}))
}catch(error){await page.screenshot({path:'/tmp/jps-reprocess-failure.png'});console.error((await page.locator('body').innerText()).slice(-4500));throw error}finally{await browser.close()}
