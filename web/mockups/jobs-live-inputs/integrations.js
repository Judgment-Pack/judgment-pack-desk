// Design fixtures only. Production must resolve the Gateway catalog, connection
// permissions, trusted Runner profile and background grant on the server.
const integrationExamples = [
  {id:'registry', name:'Case registry', provider:'MCP server', family:'cases', icon:'api', detail:'Case tools · configured Gateway operation', execution:'Background access configured'},
  {id:'project-files', name:'Local project files', provider:'This computer', family:'vault', icon:'storage', detail:'Scoped folder · incoming/life-events/', execution:'Local Runner access'},
  {id:'screening-api', name:'Screening API', provider:'HTTP API', family:'screening', icon:'api', detail:'Screening lookup · proposed Jobs adapter', execution:'Jobs support proposed'},
  {id:'document-ai', name:'Document AI', provider:'Model endpoint', family:'extract', icon:'ai', detail:'Document extraction · proposed Jobs adapter', execution:'Jobs support proposed'},
];
const integrationSelection = {cases:'registry',vault:'project-files',screening:'screening-api',extract:'document-ai'};
const integrationReview = new Set();
// These provider IDs exist in Gateway catalog v3. The mock is a fixture, not a
// connection to a live catalog. Generic HTTP/model provisioning is not invented.
const integrationCatalog = [
  {id:'aws-s3', name:'Amazon S3', description:'Read files within a configured bucket and prefix.', icon:'storage', family:'vault', action:'File selection', job:'Interactive file access; scheduled discovery is proposed.'},
  {id:'google-drive', name:'Google Drive', description:'Connect an account and select documents.', icon:'storage', action:'Document selection', job:'Interactive selection; background permission is separate.'},
  {id:'notion', name:'Notion', description:'Search and select pages from a connected workspace.', icon:'details', action:'Page search', job:'A compatible Jobs operation is required.'},
  {id:'obsidian', name:'Obsidian', description:'Read notes from a permitted local vault.', icon:'details', action:'Vault search', job:'A compatible Jobs operation is required.'},
  {id:'gmail', name:'Gmail', description:'Search and select messages from a connected account.', icon:'details', action:'Message search', job:'A compatible Jobs operation is required.'},
];
const escapeIntegration = value => String(value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
function currentIntegration(key=selected) {return integrationExamples.find(i => i.id===integrationSelection[key]);}
function integrationField(key) {
  const item=currentIntegration(key);
  return `<div class="integration-field"><label id="integration-label">Integration</label><button class="integration-current" data-change-integration aria-labelledby="integration-label integration-name">${icon(item.icon)}<span><strong id="integration-name">${escapeIntegration(item.name)}</strong><small>${escapeIntegration(item.provider)}</small></span>${icon('chevron')}</button><div class="integration-caption"><span>${escapeIntegration(item.execution)}</span><button class="subtlelink" data-add-integration>${icon('plus')} Add integration</button></div>${integrationReview.has(key)?'<p class="integration-review">Integration changed · review selection and output mapping before preview.</p>':''}</div>`;
}
function integrationPicker({addSource=false,newIntegration=false}={}) {
  document.getElementById('integration-dialog')?.remove();
  const opener=document.activeElement;
  const dialog=document.createElement('dialog');
  dialog.id='integration-dialog';dialog.className='storage-dialog integration-dialog';
  dialog.setAttribute('aria-labelledby','integration-dialog-title');
  document.body.append(dialog);
  let stage=newIntegration?'catalog':'choose', provider, created, search='';
  const compatible=item=>Boolean(item.family)&&(addSource||item.family===selected);
  const use=item=>{
    const target=addSource?item.family:selected;
    if(!compatible(item))return;
    if(integrationSelection[target]!==item.id)integrationReview.add(target);
    integrationSelection[target]=item.id;selected=target;closed=false;detailTab='configuration';
    dialog.close();render();
    document.querySelector('[data-change-integration]')?.focus();
  };
  const row=item=>`<button class="integration-option" data-use-integration="${escapeIntegration(item.id)}" ${compatible(item)?'':'disabled'}>${icon(item.icon)}<span><strong>${escapeIntegration(item.name)}</strong><small>${escapeIntegration(item.detail)}</small></span><span class="integration-option-meta">${compatible(item)?escapeIntegration(item.provider):'Not available for this source'}</span>${icon('right')}</button>`;
  const header=(title,description)=>`<header class="dialoghead"><div><h2 id="integration-dialog-title">${title}</h2><p class="muted small">${description}</p></div><button class="iconbtn" aria-label="Close integrations" data-integration-close>${icon('close')}</button></header>`;
  const footer=(extra='')=>`<footer class="dialogfooter"><span class="muted tiny">Design preview · nothing is connected or saved</span>${extra}</footer>`;
  const input=(label,id,value,help='')=>`<div class="field"><label for="${id}">${label}</label><input id="${id}" class="integration-input" value="${escapeIntegration(value)}" required>${help?`<p class="help">${help}</p>`:''}</div>`;
  function draw() {
    if(stage==='choose') {
      dialog.innerHTML=header('Choose integration','Reuse a configured connection for this job source.')+`<div class="dialogbody"><label class="integration-search">${icon('search')}<input type="search" aria-label="Search integrations" placeholder="Search integrations…" value="${escapeIntegration(search)}"></label><div class="integration-list" id="integration-results">${integrationExamples.map(row).join('')}</div><p class="muted small integration-footnote">Request settings and mappings belong to the job. Credentials stay with the integration.</p></div>`+footer('<button class="btn push" data-integration-catalog>'+icon('plus')+' Add integration</button>');
      dialog.querySelector('[type=search]').addEventListener('input',event=>{
        search=event.target.value;
        dialog.querySelector('#integration-results').innerHTML=integrationExamples.filter(item=>(item.name+' '+item.provider).toLowerCase().includes(search.toLowerCase())).map(row).join('')||'<p class="muted small integration-footnote">No matching integrations.</p>';
        bindRows();
      });
    } else if(stage==='catalog') {
      dialog.innerHTML=header('Add integration','Available from the supported Gateway catalog for this installation.')+`<div class="dialogbody"><p class="muted small integration-footnote">Only permitted providers appear here. A connection’s Jobs capabilities are checked separately.</p><div class="integration-list">${integrationCatalog.map(item=>`<button class="integration-option" data-catalog-provider="${item.id}">${icon(item.icon)}<span><strong>${item.name}</strong><small>${item.description}</small></span>${icon('right')}</button>`).join('')}</div></div>`+footer('<button class="btn push" data-integration-back>Back</button>');
    } else if(stage==='setup') {
      const extra=provider.id==='aws-s3'?`${input('Bucket','integration-bucket','business-evidence')}${input('Allowed prefix','integration-prefix','cases/','Job file selectors stay inside this scope.')}${input('Region','integration-region','us-east-1')}<div class="field"><label>Credential</label><div class="value">Example saved credential</div><p class="help">Production uses Gateway’s credential setup. Do not enter real secrets in this mock.</p></div>`:provider.id==='obsidian'?input('Vault folder','integration-vault','/workspace/notes','Production uses the permitted local folder setup.'):'<div class="notice integration-footnote">Production opens the existing Gateway account setup and sign-in flow, then returns here. This preview simulates that step.</div>';
      dialog.innerHTML=header(`Add ${provider.name}`,'Connect once. Reuse the integration across jobs and source mappings.')+`<form id="integration-setup"><div class="dialogbody">${input('Integration name','integration-display-name',provider.id==='aws-s3'?'Business evidence':provider.name+' workspace')}${extra}<p class="integration-review">${provider.job}</p></div>`+footer('<button type="button" class="btn push" data-integration-back>Back</button><button class="btn primary" type="submit">'+(provider.id==='aws-s3'||provider.id==='obsidian'?'Save example integration':'Simulate connection')+'</button>')+'</form>';
      dialog.querySelector('form').addEventListener('submit',event=>{
        event.preventDefault();
        const name=dialog.querySelector('#integration-display-name').value.trim();
        if(!name)return;
        created={id:'example-'+provider.id+'-'+integrationExamples.length,name,provider:provider.name,family:provider.family,icon:provider.icon,detail:provider.action+' · example connection',execution:provider.id==='aws-s3'?'Interactive only · background access needed':'Jobs operation required',bucket:dialog.querySelector('#integration-bucket')?.value,prefix:dialog.querySelector('#integration-prefix')?.value};
        integrationExamples.push(created);stage='connected';draw();
      });
    } else {
      dialog.innerHTML=header('Integration added to preview',escapeIntegration(created.name))+`<div class="dialogbody"><div class="integration-created">${icon('check')}<div><strong>${escapeIntegration(created.provider)}</strong><p class="muted small">${escapeIntegration(created.execution)}</p></div></div><p class="muted small integration-footnote">The job’s draft and mappings are preserved. Selecting a different integration requires a new preview.</p><div class="notice">${provider.job}${created.family?' You can review the proposed source configuration here.':' This integration remains in Connections until a compatible Jobs source is supported.'}</div></div>`+footer(`<button class="btn push" data-integration-done>Back to integrations</button>${compatible(created)?'<button class="btn primary" data-use-integration="'+created.id+'">Use integration</button>':''}`);
    }
    dialog.querySelectorAll('[data-integration-close]').forEach(el=>el.onclick=()=>dialog.close());
    dialog.querySelectorAll('[data-integration-catalog]').forEach(el=>el.onclick=()=>{stage='catalog';draw();});
    dialog.querySelectorAll('[data-integration-back]').forEach(el=>el.onclick=()=>{stage=stage==='setup'?'catalog':'choose';draw();});
    dialog.querySelectorAll('[data-integration-done]').forEach(el=>el.onclick=()=>{stage='choose';draw();});
    dialog.querySelectorAll('[data-catalog-provider]').forEach(el=>el.onclick=()=>{provider=integrationCatalog.find(p=>p.id===el.dataset.catalogProvider);stage='setup';draw();});
    bindRows();
  }
  function bindRows() {dialog.querySelectorAll('[data-use-integration]').forEach(el=>el.onclick=()=>use(integrationExamples.find(i=>i.id===el.dataset.useIntegration)));}
  dialog.addEventListener('close',()=>{dialog.remove();if(opener?.isConnected)opener.focus();});
  draw();dialog.showModal();
}
