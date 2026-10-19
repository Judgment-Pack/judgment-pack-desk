import assert from 'node:assert/strict'
import { chromium } from 'playwright-core'
const [origin,destination,phase,mode]=process.argv.slice(2)
assert(origin&&destination&&phase,'Use the disposable Go TestProjectMoveBrowser fixture')
const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',args:['--no-sandbox']})
const errors=[]
try {
 const page=await browser.newPage({viewport:{width:1440,height:1000},colorScheme:'dark'})
 page.setDefaultTimeout(12_000)
 page.on('pageerror',error=>errors.push(error.message))
 await page.goto(origin+'/admin#general')
 const move=page.getByRole('button',{name:'Move to…',exact:true})
 await move.waitFor()
 await page.waitForFunction(()=>[...document.querySelectorAll('button')].some(button=>button.textContent==='Move to…'&&!button.disabled))
 if(phase==='restarted'){
  assert(await page.getByText(destination,{exact:true}).count()>0)
  assert.equal(await page.getByRole('button',{name:/Restart required · Project moved/}).count(),0)
  assert.deepEqual(errors,[])
  console.log(JSON.stringify({restartDefault:true,correctLocation:true,pageErrors:0}))
 } else {
  await move.click()
  let dialog=page.getByRole('dialog',{name:'Move startup project',exact:true})
  if(mode==='default'){
   await dialog.getByRole('button',{name:'Use default location',exact:true}).click()
   await dialog.getByText(destination,{exact:true}).waitFor()
  } else {
   await dialog.getByRole('button',{name:'Choose folder…',exact:true}).click()
   await dialog.getByText(destination,{exact:true}).waitFor()
  }
  assert.equal(await dialog.getByRole('textbox').count(),0)
  for(const width of [1440,390,320]){
   await page.setViewportSize({width,height:900})
   const box=await dialog.boundingBox(),action=await dialog.getByRole('button',{name:'Review move',exact:true}).boundingBox()
   assert(box&&box.x>=0&&box.x+box.width<=width+1)
   assert(action&&action.y+action.height<=900)
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'horizontal overflow')
   for(const theme of ['dark','light']){
    await page.emulateMedia({colorScheme:theme})
    await page.screenshot({path:`/tmp/jps-project-move-${mode}-${theme}-${width}.png`,animations:'disabled'})
   }
  }
  await dialog.getByRole('button',{name:'Review move',exact:true}).click()
  dialog=page.getByRole('dialog',{name:'Review project move',exact:true})
  await dialog.waitFor()
  assert.equal(await dialog.getByText(destination,{exact:true}).count(),1)
  await page.screenshot({path:'/tmp/jps-project-move-review-320.png',animations:'disabled'})
  await dialog.getByRole('button',{name:'Back',exact:true}).click()
  dialog=page.getByRole('dialog',{name:'Move startup project',exact:true})
  await dialog.getByText(destination,{exact:true}).waitFor()
  await dialog.getByRole('button',{name:'Review move',exact:true}).click()
  dialog=page.getByRole('dialog',{name:'Review project move',exact:true})
  await dialog.getByRole('button',{name:'Move project',exact:true}).click()
  await dialog.waitFor({state:'hidden'})
  const notice=page.getByRole('button',{name:/Restart required · Project moved/})
  try { await notice.waitFor() } catch(error) { console.log(JSON.stringify({url:page.url(),body:(await page.locator('body').innerText()).slice(-6000),errors}));await page.screenshot({path:'/tmp/jps-project-move-error.png'}); throw error }
  await notice.click()
  await page.getByText('Stop Desk, then start it with the new project folder. Refreshing this page does not restart Desk.',{exact:true}).waitFor()
  assert(await move.isDisabled())
  await page.screenshot({path:'/tmp/jps-project-move-done-320.png',animations:'disabled'})
  assert.deepEqual(errors,[])
  console.log(JSON.stringify({review:true,explicitConfirmation:true,widths:[1440,390,320],restartNotice:true,pageErrors:0}))
 }
}finally{await browser.close()}
