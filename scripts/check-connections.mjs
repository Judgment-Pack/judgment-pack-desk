import { createRequire } from 'node:module';
import { mkdtemp, mkdir, cp, writeFile, readFile, rm } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const repo=resolve(fileURLToPath(new URL('..', import.meta.url)));
const { DESK_BUNDLE, JPACK_BINARY, JPACK_FIXTURE_PROJECT, CHROMIUM_PATH }=process.env;
if(!DESK_BUNDLE||!JPACK_BINARY||!JPACK_FIXTURE_PROJECT||!CHROMIUM_PATH) throw Error('Set DESK_BUNDLE, JPACK_BINARY, JPACK_FIXTURE_PROJECT, and CHROMIUM_PATH. The project must be a synthetic fixture.');
const out=resolve(process.env.SMOKE_OUTPUT ?? '/tmp/desk-connections-review');await mkdir(out,{recursive:true});
const port=process.env.SMOKE_PORT ?? '18889';
const { chromium } = createRequire(`${repo}/web/package.json`)('playwright-core');
const work=await mkdtemp('/tmp/jp-unified-smoke-'), origin=`http://127.0.0.1:${port}`, secret=randomBytes(24).toString('hex');
await cp(JPACK_FIXTURE_PROJECT,`${work}/project`,{recursive:true});
for(const dir of ['config','data','vault','vault/.obsidian','vault/Policies'])await mkdir(`${work}/${dir}`);
await writeFile(`${work}/vault/Policies/Travel.md`,'# Travel policy\n\nKeep receipts. Manager approval is required over 100.\n');
await writeFile(`${work}/vault/.obsidian/hidden.md`,'Never include this configuration note.');
const server=spawn(`${DESK_BUNDLE}/jpack-desk`,['--port',port,'--dev-token',secret,'--print-url=false','--jpack',JPACK_BINARY,`${work}/project`],{env:{...process.env,XDG_CONFIG_HOME:`${work}/config`,XDG_DATA_HOME:`${work}/data`},stdio:'ignore'});
const api=(path,init={})=>fetch(origin+path,{...init,headers:{Authorization:`Bearer ${secret}`,...init.headers}});
let browser,page;const findings={};
try {
 for(let i=0;i<150;i++){try{if((await api('/api/conversations')).ok)break}catch{}await new Promise(r=>setTimeout(r,100))}
 browser=await chromium.launch({executablePath:CHROMIUM_PATH,args:['--no-sandbox']});
 const context=await browser.newContext({colorScheme:'dark',viewport:{width:1440,height:900}});page=await context.newPage();page.setDefaultTimeout(15000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/desk-config',async route=>{const response=await route.fetch();const data=await response.json();data.present=true;data.content=JSON.stringify({deskConfigVersion:1,assistant:{endpoint:{url:'https://model.invalid/v1',kind:'openai-compatible',model:'synthetic-model',models:['synthetic-model'],tools:['validate']}}});await route.fulfill({response,json:data})});
 await page.goto(`${origin}/launch?secret=${secret}`);
 await page.getByRole('heading',{name:'What would you like to work on?',exact:true}).waitFor();
 const input=page.getByRole('textbox',{name:'Message the assistant',exact:true});await input.fill('Keep my unsent draft.');
 await page.getByRole('button',{name:'Attach files',exact:true}).click();
 await page.getByRole('menuitem',{name:'More connections',exact:true}).click();
 await page.getByRole('button',{name:/Obsidian/}).click();
 const dialog=page.locator('#desk-inspector');await dialog.getByRole('textbox',{name:'Vault folder'}).fill(`${work}/vault`);
 await dialog.getByText('How it works',{exact:true}).click();await page.screenshot({path:`${out}/connect.png`});
 await dialog.getByRole('button',{name:'Connect',exact:true}).click();
 const row=dialog.getByRole('checkbox');await row.waitFor();if(await row.count()!==1)throw Error('Hidden vault note leaked');
 await row.check();await page.screenshot({path:`${out}/select.png`});
 await dialog.getByRole('button',{name:'Attach 1 item',exact:true}).click();
 await page.getByRole('button',{name:'Travel.txt',exact:true}).waitFor();
 findings.composerFocused=await input.evaluate(el=>document.activeElement===el);
 findings.draftPreserved=await input.inputValue()==='Keep my unsent draft.';
 await page.getByRole('button',{name:'Travel.txt',exact:true}).click();
 const preview=page.getByRole('dialog',{name:'Travel.txt',exact:true});await preview.getByText(/Manager approval is required over 100/).waitFor();
 findings.verified=await preview.getByText(/Receipt verified/).count()===1;
 await page.screenshot({path:`${out}/attached.png`});
 await page.keyboard.press('Escape');
 findings.unsentChats=(await(await api('/api/conversations')).json()).chats?.length??0;
 // Connection management removes access without rewriting retained attachments.
 const disconnected=await api('/api/connections/obsidian/disconnect',{method:'POST',headers:{'Content-Type':'application/json'},body:'{}'});if(!disconnected.ok)throw Error('Disconnect failed');
 await writeFile(`${work}/vault/Policies/Travel.md`,'Changed source after attachment.');
 await page.getByRole('button',{name:'Travel.txt',exact:true}).click();await preview.getByText(/Manager approval is required over 100/).waitFor();findings.retainedAfterEditAndDisconnect=true;await page.keyboard.press('Escape');
 // A route change must dismiss contextual connections without a modal trap.
 await page.getByRole('button',{name:'Attach files',exact:true}).click();
 await page.getByRole('menuitem',{name:'More connections',exact:true}).click();
 await page.getByRole('button',{name:/Obsidian/}).click();
 await page.getByRole('link',{name:/^Packs/}).click();
 await page.locator('#desk-inspector').getByRole('textbox',{name:'Vault folder'}).waitFor({state:'hidden'});
 findings.routeChangeClosedConnections=true;
 await page.goto(origin);
 findings.locales=[];
 for(const locale of ['en','fr','es','de','it','pt-PT','pt-BR','ko','zh-Hans','zh-Hant','yue-Hant','ja']){
  const msgs=JSON.parse(await readFile(`${repo}/web/src/i18n/locales/${locale}.json`,'utf8'));
  await page.evaluate(locale=>localStorage.setItem('jpack-desk.language.v1',locale),locale);await page.setViewportSize({width:390,height:720});await page.reload();await page.waitForLoadState('networkidle');
  await page.getByRole('button',{name:msgs['Attach files'],exact:true}).click();await page.getByRole('menuitem',{name:msgs['More connections'],exact:true}).click();await page.getByRole('button',{name:/Obsidian/}).click();
  const popup=page.locator('#desk-inspector');await popup.getByRole('textbox',{name:msgs['Vault folder']}).waitFor();const box=await popup.boundingBox();
  if(!box||box.x<0||box.x+box.width>391||await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Missing pane or overflow '+locale);
  findings.locales.push(locale);if(locale==='ja')await page.screenshot({path:`${out}/narrow-ja.png`});await page.keyboard.press('Escape');
 }
 findings.errors=errors;if(!findings.composerFocused||!findings.draftPreserved||!findings.verified||!findings.routeChangeClosedConnections||errors.length)throw Error('Interaction regression');
 await writeFile(`${out}/findings.json`,JSON.stringify(findings,null,2));console.log(JSON.stringify(findings,null,2));
} catch(error){if(page){await page.screenshot({path:`${out}/failure.png`});await writeFile(`${out}/failure.txt`,await page.locator('body').innerText())}throw error}
finally{await browser?.close();server.kill('SIGTERM');await new Promise(r=>setTimeout(r,500));await rm(work,{recursive:true,force:true})}
