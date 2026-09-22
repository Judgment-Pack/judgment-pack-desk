// Real Desk + real signing gateway; only the upstream provider is synthetic.
import { createRequire } from 'node:module';
import { mkdtemp, mkdir, cp, writeFile, readFile, rm, chmod } from 'node:fs/promises';
import { spawn, spawnSync } from 'node:child_process';
import { createHash, randomBytes } from 'node:crypto';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const repo=resolve(fileURLToPath(new URL('..',import.meta.url)));
const {DESK_BUNDLE,JPACK_BINARY,JPACK_FIXTURE_PROJECT,CHROMIUM_PATH}=process.env;
if(!DESK_BUNDLE||!JPACK_BINARY||!JPACK_FIXTURE_PROJECT||!CHROMIUM_PATH)throw Error('Set the bundle, runtime, synthetic project and Chromium paths.');
const out=resolve(process.env.SMOKE_OUTPUT??'/tmp/desk-generic-connections-review');await mkdir(out,{recursive:true});
const work=await mkdtemp('/tmp/jp-generic-smoke-'),bundle=`${work}/bundle`;
const hash=value=>createHash('sha256').update(value).digest('hex');
const originalDesk=hash(await readFile(`${DESK_BUNDLE}/jpack-desk`));
await cp(DESK_BUNDLE,bundle,{recursive:true});await cp(JPACK_FIXTURE_PROJECT,`${work}/project`,{recursive:true});
for(const dir of ['config','data'])await mkdir(`${work}/${dir}`);
const descriptor={id:'fixture-files',protocol:'connection-v1',auth:'credentials',registration:'form',selection:'source-search',queryRequired:false,operations:['status','configure','search','select','disconnect'],presentation:{name:'Fixture files',icon:'',description:{en:'Synthetic source for acceptance testing.'},instructions:{en:'Use the fixture folder and key.'}},setup:[{key:'folder',type:'text',label:{en:'Folder'},required:true},{key:'key',type:'password',label:{en:'Access key'},required:true}],authorizationEndpoints:[],source:{id:'fixture-files',shape:'command',record:'resource-v1'}};
await writeFile(`${bundle}/catalog.json`,JSON.stringify({version:3,sources:[],providers:[descriptor]}));
await writeFile(`${bundle}/plan.json`,JSON.stringify({version:1,sources:[{id:'documents',executable:'adapter-document',args:[],shape:'command',timeout:40,connections:false},{id:'fixture-files',executable:'adapter-fixture',args:[],shape:'command',timeout:60,connections:true}]}));
await cp(`${repo}/web/src/documents/__fixtures__/resource-snapshot.json`,`${bundle}/resource.json`);
const build=spawnSync('go',['build','-trimpath','-o',`${bundle}/gateway-connections`,`${repo}/scripts/fixtures/generic-connection.go`],{env:process.env,encoding:'utf8'});
if(build.status!==0)throw Error(build.stderr);
await cp(`${bundle}/gateway-connections`,`${bundle}/adapter-fixture`);await chmod(`${bundle}/adapter-fixture`,0o755);
const manifest=JSON.parse(await readFile(`${bundle}/gateway-bundle.json`,'utf8'));
manifest.revision='synthetic-independent-gateway';
for(const name of ['gateway-connections','adapter-fixture'])manifest.files[name]=hash(await readFile(`${bundle}/${name}`));
const manifestRaw=JSON.stringify(manifest,null,2)+'\n';await writeFile(`${bundle}/gateway-bundle.json`,manifestRaw);
const secret=randomBytes(24).toString('hex'),port=process.env.SMOKE_PORT??'18892',origin=`http://127.0.0.1:${port}`;
const server=spawn(`${bundle}/jpack-desk`,['--port',port,'--dev-token',secret,'--print-url=false','--jpack',JPACK_BINARY,`${work}/project`],{env:{...process.env,XDG_CONFIG_HOME:`${work}/config`,XDG_DATA_HOME:`${work}/data`,JPACK_DESK_GATEWAY_MANIFEST_SHA256:hash(manifestRaw)},stdio:'ignore'});
const api=(path,init={})=>fetch(origin+path,{...init,headers:{Authorization:`Bearer ${secret}`,...init.headers}});
let browser,page;const findings={};
try {
 for(let i=0;i<150;i++){try{if((await api('/api/conversations')).ok)break}catch{}await new Promise(r=>setTimeout(r,100))}
 const {chromium}=createRequire(`${repo}/web/package.json`)('playwright-core');
 browser=await chromium.launch({executablePath:CHROMIUM_PATH,args:['--no-sandbox']});
 page=await browser.newPage({colorScheme:'dark',viewport:{width:1440,height:900}});page.setDefaultTimeout(15000);
 const errors=[];page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/desk-config',async route=>{const response=await route.fetch();const data=await response.json();data.present=true;data.content=JSON.stringify({deskConfigVersion:1,assistant:{endpoint:{url:'https://model.invalid/v1',kind:'openai-compatible',model:'synthetic-model',models:['synthetic-model'],tools:['validate']}}});await route.fulfill({response,json:data})});
 await page.goto(`${origin}/launch?secret=${secret}`);
 const input=page.getByRole('textbox',{name:'Message the assistant',exact:true});await input.fill('Keep this draft while connecting.');
 await page.getByRole('button',{name:'Attach files',exact:true}).click();await page.getByRole('menuitem',{name:'More connections',exact:true}).click();
 await page.getByRole('button',{name:/Fixture files/}).click();
 const pane=page.locator('#desk-inspector');await pane.getByLabel('Folder',{exact:true}).fill('policies');await pane.getByLabel('Access key',{exact:true}).fill('fixture-only-key');
 await page.screenshot({path:`${out}/setup.png`});await pane.getByRole('button',{name:'Connect',exact:true}).click();
 await pane.getByRole('checkbox').check();await page.screenshot({path:`${out}/select.png`});
 await pane.getByRole('button',{name:'Attach 1 item',exact:true}).click();
 await page.getByRole('button',{name:'Policy.txt',exact:true}).waitFor();
 findings.draftPreserved=await input.inputValue()==='Keep this draft while connecting.';
 findings.composerFocused=await input.evaluate(el=>document.activeElement===el);
 await page.getByRole('button',{name:'Policy.txt',exact:true}).click();
 const preview=page.getByRole('dialog',{name:'Policy.txt',exact:true});await preview.getByText('Fixture policy.',{exact:true}).waitFor();
 findings.verified=await preview.getByText(/Receipt verified/).count()===1;await page.screenshot({path:`${out}/verified.png`});
 findings.deskBinaryUnchanged=originalDesk===hash(await readFile(`${bundle}/jpack-desk`));
 findings.unsentChats=(await(await api('/api/conversations')).json()).chats?.length??0;
 findings.errors=errors;
 if(!findings.draftPreserved||!findings.composerFocused||!findings.verified||!findings.deskBinaryUnchanged||findings.unsentChats!==0||errors.length)throw Error('Generic connection acceptance failed');
 await writeFile(`${out}/findings.json`,JSON.stringify(findings,null,2));console.log(JSON.stringify(findings,null,2));
}catch(error){if(page){await page.screenshot({path:`${out}/failure.png`});await writeFile(`${out}/failure.txt`,await page.locator('body').innerText())}throw error}
finally{await browser?.close();server.kill('SIGTERM');await new Promise(r=>setTimeout(r,500));await rm(work,{recursive:true,force:true})}
