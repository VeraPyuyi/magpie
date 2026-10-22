const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const {test} = require('node:test');
const {chromium, webkit} = require('playwright');
const assets = path.resolve(__dirname, '../assets');
for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ['chromium','webkit']) {
 for (const scenario of ['selection', 'refresh', 'language', 'progress']) test(`${engine}: Dot review ${scenario}`, async t => {
  const browser = await (engine === 'webkit' ? webkit.launch() : chromium.launch({channel:'chromium'}));
  t.after(()=>browser.close());
  for (const lang of ['en','zh','zh-TW','ja','de']) for (const width of [1100,420]) {
   const context = await browser.newContext({viewport:{width,height:740},reducedMotion:'reduce'});
   const page = await context.newPage(); page.setDefaultTimeout(5000);
   let failB=false, holdA=false, held=null, state='running', posts=0, gets=0, cancels=0;
   const identity = id => ({request_id:id,session_id:'session-'+id,job_id:'job-'+id,mode:'task',digest:'a'.repeat(64),state:'accepted',block_replay:true});
   const job = id => ({version:1,request_id:id,session_id:'session-'+id,job_id:'job-'+id,mode:'task',request_digest:'a'.repeat(64),summary:scenario==='progress'?'<img src=x onerror="window.progressPwned=1">'+ 's'.repeat(5000):'',state:id==='A'?'completed':state,...(id==='A'||state==='completed'?{result:{text:'output-'+id,artifacts:[{url:'https://artifact.invalid/report',label:'artifact-'+id},{url:'file:///C:/private',label:'file'},{url:'https://user:pass@artifact.invalid/private',label:'userinfo'}]}}:{})});
   await page.route('**/*',async route=>{
    const r=route.request(), u=new URL(r.url()), json=data=>route.fulfill({json:data});
    if(u.pathname==='/boot.js')return route.fulfill({contentType:'text/javascript',body:`window.bootPrefs={lang:${JSON.stringify(lang)},theme:'light'};`});
    if(u.pathname==='/wails/runtime.js')return route.fulfill({contentType:'text/javascript',body:'export const Window={};'});
    if(u.pathname==='/api/state')return json({agents:[],profiles:[],settings:{lang,theme:'light'}});
    if(u.pathname==='/api/providers')return json({providers:[],presets:[],gateway:{running:true,url:'http://127.0.0.1:3999',calls:[],groups:[]}});
    if(u.pathname==='/api/dot/status')return json({enabled:true,code:'ready',task_available:true,capabilities:{tasks:true},capacity:{limit:100,retained:7,remaining:93}});
    if(u.pathname==='/api/dot/requests')return json(['A','B'].map(identity));
    if(u.pathname==='/api/dot/jobs'){posts++;return route.fulfill({status:409,json:{code:'outcome_unknown'}});}
    if(u.pathname.startsWith('/api/dot/requests/')) {
     const id=u.pathname.split('/')[4];
     if(u.pathname.endsWith('/cancel')) {cancels++;return json({code:'cancel_intent_acknowledged'});}
     gets++;
     if(id==='A'&&holdA){held=()=>json(job(id));return;}
     if(id==='B'&&failB)return route.fulfill({status:409,json:{code:'outcome_unknown'}});
     return json(job(id));
    }
    if(u.pathname.startsWith('/api/'))return json({});
    if(u.host!=='magpie.test')return route.fulfill({status:404,body:''});
    const file=path.join(assets,u.pathname==='/'?'index.html':u.pathname);
    try{return route.fulfill({body:await fs.readFile(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]});}catch{return route.fulfill({status:404,body:''});}
   });
   await page.goto('http://magpie.test/?view=dot');
   await page.locator('#dot-history button').first().waitFor();
   const row = id => page.locator('.dot-request').filter({has:page.locator('span', {hasText:new RegExp('^'+id+' ·')})});
   const lookup = async id => {await row(id).locator('button').first().focus();await page.keyboard.press('Enter');};
   const expected = key => page.evaluate(key=>{const value=t(key);return value===key&&key.startsWith('Dot state: ')?key.slice(11).replaceAll('_',' '):value;},key);
   if(scenario==='selection') {
    await lookup('A');await page.locator('#dot-result pre').waitFor();
    assert.equal(await page.locator('#dot-result a').count(),1);
    failB=true;await lookup('B');
    await page.waitForFunction(text=>document.querySelector('#dot-notice').textContent===text,await expected('Dot state: outcome_unknown'));
    assert.equal(await page.locator('#dot-result pre, #dot-result a').count(),0,'failed B must clear A output/artifacts');
    assert.equal(await page.locator('#dot-result').textContent(),'','failed B must clear A completed state');
    failB=false;holdA=true;await lookup('A');
    await page.waitForFunction(()=>document.querySelector('#dot-history')!==null); // response release is controlled below
    for(let attempt=0;!held&&attempt<100;attempt++)await new Promise(resolve=>setTimeout(resolve,5));
    assert.ok(held,'A lookup reached bridge fixture');
    await lookup('B');await page.waitForFunction(()=>document.querySelector('#dot-result').textContent.includes('job-B'));
    await held(); await page.waitForLoadState('networkidle');
    assert.ok(!(await page.locator('#dot-result').textContent()).includes('job-A'),'late A cannot replace selected B');
    assert.equal(await page.locator('#dot-new, #dot-task, #dot-submit, #dot-context').count(),0);
   } else if(scenario==='refresh') {
    await lookup('B');await page.waitForFunction(()=>document.querySelector('#dot-result').textContent.includes('job-B'));
    await row('B').locator('button').last().focus();await page.keyboard.press('Enter');
    await page.waitForFunction(()=>document.querySelector('#dot-notice').textContent===t('Cancellation is intent only; refresh to inspect completion'));
    assert.equal(cancels,1);assert.ok((await page.locator('#dot-result').textContent()).includes(await expected('Dot state: running')));
    for(const next of ['cancelled','running','completed']) {
     state=next;const before=gets;
     await page.locator('#dot-refresh').focus();await page.keyboard.press('Enter');
     await page.waitForFunction(label=>document.querySelector('#dot-result').textContent.includes(label),await expected('Dot state: '+next));
     assert.ok(gets>before,'Refresh GETs selected original request');
    }
    assert.equal(posts,0,'refresh never submits');
   } else if(scenario==='progress') {
    await lookup('B');await page.locator('#dot-result .dot-summary').waitFor();
    const summary=await page.locator('#dot-result .dot-summary').textContent();
    assert.ok(summary.startsWith('<img src=x'));assert.ok(summary.length<=4096);
    assert.equal(await page.locator('#dot-result img').count(),0);
    assert.ok((await page.locator('#dot-capacity').textContent()).includes('100'));
    assert.ok((await page.locator('#dot-capacity').textContent()).includes('93'));
    assert.ok((await page.locator('#dot-capacity').textContent()).includes('7'));
    assert.equal(await page.evaluate(()=>window.progressPwned),undefined);
   } else {
    await lookup('A');await page.locator('#dot-result pre').waitFor();
    const next=lang==='de'?'ja':'de';
    await page.evaluate(next=>{applyPrefs({lang:next,theme:'light'});magpie.call('dot','relang');},next);
    assert.equal(await row('A').locator('button').last().textContent(),await expected('Request cancellation'),'existing history action relanguages');
    assert.ok((await page.locator('#dot-result').textContent()).includes(await expected('Dot state: completed')));
    assert.equal(await page.locator('#dot-status').textContent(),await expected('Dot state: ready'));
    assert.equal(await page.locator('#dot-capability').textContent(),await expected('Task capability verified'));
    assert.equal(await page.locator('#dot-notice').textContent(),await expected('Dot request recovered'));
   }
   await context.close();
  }
 });
}
