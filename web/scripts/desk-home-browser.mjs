import assert from 'node:assert/strict'
import { chromium } from 'playwright-core'
const [origin,destination,phase,mode]=process.argv.slice(2)
assert(origin&&destination&&phase,'Use only the disposable TestDeskHomeBrowser fixture')
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',args:['--no-sandbox']})
const errors=[]
try{
 const page=await browser.newPage({viewport:{width:1440,height:1000},colorScheme:'dark'})
 page.setDefaultTimeout(12_000);page.on('pageerror',error=>errors.push(error.message))
 await page.goto(origin+'/admin#general')
 const move=page.getByRole('button',{name:'Move…',exact:true});await move.waitFor()
 if(phase==='restarted'){
  await page.getByText(destination,{exact:true}).waitFor()
  assert.equal(await page.getByRole('button',{name:'Restart required · Desk home move',exact:true}).count(),0)
 }else{
  await move.click()
  let dialog=page.getByRole('dialog',{name:'Move Desk home',exact:true})
  await dialog.getByRole('button',{name:mode==='default'?'Use default location':'Choose folder…',exact:true}).click()
  await dialog.getByText(destination,{exact:true}).waitFor()
  assert.equal(await dialog.getByRole('textbox').count(),0)
  for(const width of [1440,390,320]){
   await page.setViewportSize({width,height:900})
   const box=await dialog.boundingBox(),action=await dialog.getByRole('button',{name:'Review move',exact:true}).boundingBox()
   assert(box&&box.x>=0&&box.x+box.width<=width+1)
   assert(action&&action.y+action.height<=900)
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'horizontal overflow')
   for(const theme of ['dark','light']){await page.emulateMedia({colorScheme:theme});await page.screenshot({path:`/tmp/jps-home-${mode}-${theme}-${width}.png`,animations:'disabled'})}
  }
  await dialog.getByRole('button',{name:'Review move',exact:true}).click()
  dialog=page.getByRole('dialog',{name:'Review Desk home move',exact:true});await dialog.waitFor()
  await dialog.getByText(destination,{exact:true}).waitFor()
  await page.screenshot({path:`/tmp/jps-home-${mode}-review-320.png`,animations:'disabled'})
  await dialog.getByRole('button',{name:'Move Desk home',exact:true}).click()
  await dialog.waitFor({state:'hidden'})
  await page.getByRole('button',{name:'Restart required · Desk home move',exact:true}).waitFor()
  assert(await move.isDisabled())
 }
 assert.deepEqual(errors,[])
 console.log(JSON.stringify({mode,phase,pageErrors:0,nativeSelection:true,explicitReview:true,widths:[1440,390,320]}))
}finally{await browser.close()}
