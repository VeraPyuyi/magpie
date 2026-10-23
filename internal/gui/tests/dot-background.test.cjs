const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const {test} = require('node:test');
const {chromium,webkit} = require('playwright');
const assets = process.env.MAGPIE_DOT_ASSETS || path.resolve(__dirname,'../assets');
for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ['chromium','webkit']) {
 test(`${engine}: Dot has no manual submission; readiness, history, literal results and pairing remain`, async t => {
  const browser = await (engine==='webkit'?webkit.launch():chromium.launch({channel:'chromium'}));
  t.after(()=>browser.close());
  for(const lang of ['en','zh','zh-TW','ja','de']) for(const width of [1100,420]) {
   const context=await browser.newContext({viewport:{width,height:740},reducedMotion:'reduce'});
   const page=await context.newPage();page.setDefaultTimeout(7000);
   let enabled=true, available=false, posts=[], gets=[], cancelled=0;
   const identity={request_id:'gui-existing',session_id:'session-existing'};
   const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.route('**/*',async route=>{
    const r=route.request(), u=new URL(r.url());const json=data=>route.fulfill({json:data});
    if(u.pathname==='/boot.js')return route.fulfill({contentType:'text/javascript',body:`window.bootPrefs={lang:${JSON.stringify(lang)},theme:'light'};`});
    if(u.pathname==='/wails/runtime.js')return route.fulfill({contentType:'text/javascript',body:'export const Window={};'});
    if(u.pathname==='/api/state')return json({agents:[],profiles:[],settings:{lang,theme:'light'}});
    if(u.pathname==='/api/providers')return json({providers:[],presets:[],gateway:{running:true,url:'http://127.0.0.1:3999',calls:[],groups:[]}});
    if(u.pathname==='/api/dot/status')return json({enabled,code:!enabled?'disabled':available?'ready':'unverified',task_available:available,subscription_term:enabled?'permanent':'',capabilities:{tasks:available}});
    if(u.pathname==='/api/dot/requests')return json(identity?[{request_id:identity.request_id,session_id:identity.session_id,mode:'task',digest:'a'.repeat(64),state:'acceptance_unknown',block_replay:true}]:[]);
    if(u.pathname==='/api/dot/jobs'){posts.push(r.postDataJSON());return route.fulfill({status:409,json:{code:'outcome_unknown'}});}
    if(u.pathname.startsWith('/api/dot/requests/')){
     gets.push(u.pathname);
     if(u.pathname.endsWith('/cancel')){cancelled++;return json({code:'cancel_intent_acknowledged'});}
     return json({version:1,request_digest:'a'.repeat(64),request_id:identity.request_id,session_id:identity.session_id,mode:'task',job_id:'job-fixture',state:'completed',result:{text:'<img src=x onerror="window.pwned=1">',artifacts:[{url:'javascript:alert(1)',label:'unsafe'},{url:'https://artifact.invalid/report',label:'Report'}]}});
    }
    if(u.pathname==='/api/dot/off'){enabled=false;available=false;return json({enabled:false});}
     if(u.pathname==='/api/dot/gateway/requests')return json([{request_id:'gateway-fixture',mode:'task',state:'accepted'}]);
     if(u.pathname==='/api/dot/gateway/requests/gateway-fixture')return json({request_id:'gateway-fixture',job_id:'cloud-job',mode:'task',state:'completed',result:{text:'gateway literal'}});
    if(u.pathname.startsWith('/api/'))return json({});
    if(u.host!=='magpie.test')return route.fulfill({status:404,body:''});
    const file=path.join(assets,u.pathname==='/'?'index.html':u.pathname);
    try{return route.fulfill({body:await fs.readFile(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]});}catch{return route.fulfill({status:404,body:''});}
   });
   await context.addInitScript(identity=>localStorage.setItem('magpie.dot.pending',JSON.stringify(identity)),identity);
   await page.goto('http://magpie.test/?view=dot');
   await page.locator('#dot-refresh').waitFor();
   assert.equal(await page.locator('#dot-submit, #dot-task, #dot-context, #dot-new, #view-dot input[type=file]').count(),0,'manual submission controls must be absent');
   assert.ok((await page.locator('#dot-status').textContent()).length);
   await page.locator('#dot-history button').first().waitFor();
   assert.ok((await page.locator('#dot-pair').textContent()).length>10);
   available=true;await page.locator('#dot-refresh').click();
   await page.waitForFunction(()=>document.querySelector('#dot-status').textContent.startsWith((t('Dot state: ready')==='Dot state: ready'?'ready':t('Dot state: ready'))));
   const label={en:'Permanent subscription',zh:'订阅无到期','zh-TW':'訂閱無到期',ja:'無期限のサブスクリプション',de:'Unbefristetes Abonnement'}[lang];
   assert.ok((await page.locator('#dot-status').textContent()).includes(label));
   await page.reload();await page.locator('#dot-history button').first().waitFor();
   await page.locator('#dot-result pre').waitFor();
   assert.ok((await page.locator('#dot-identity').textContent()).includes(identity.request_id));
   await page.locator('#dot-lookup').click();await page.locator('#dot-result pre').waitFor();
   await page.locator('#dot-history button').first().click();await page.locator('#dot-result pre').waitFor();
   assert.equal(await page.locator('#dot-result pre').textContent(),'<img src=x onerror="window.pwned=1">');
   assert.equal(await page.locator('#dot-result img').count(),0);assert.equal(await page.locator('#dot-result a').count(),1);
   assert.equal(posts.length,0);assert.ok(gets.some(p=>p.endsWith(identity.request_id)));
   await page.locator('#dot-history button').last().focus(); await page.keyboard.press('Enter');
   await page.waitForFunction(()=>document.querySelector('#dot-notice').textContent===t('Cancellation is intent only; refresh to inspect completion')); assert.equal(cancelled,1);
   assert.equal(await page.evaluate(()=>window.pwned),undefined);
   await page.locator('#dot-off').click();
   await page.waitForFunction(()=>document.querySelector('#dot-status').textContent===(t('Dot state: disabled')==='Dot state: disabled'?'disabled':t('Dot state: disabled')));
   assert.ok(await page.locator('#view-dot').evaluate(n=>n.scrollWidth<=n.clientWidth+1));
    await page.locator('#view-dot .dot-actions button').filter({hasText:await page.evaluate(()=>t('Routed through Magpie'))}).focus(); await page.keyboard.press('Enter');
    await page.waitForFunction(()=>document.querySelector('#dot-history')?.textContent.includes('gateway-fixture'));
    assert.equal(await page.locator('#dot-history button').count(),1,'gateway source has lookup only');
    await page.locator('#dot-history button').focus(); await page.keyboard.press('Enter');
    await page.waitForFunction(()=>document.querySelector('#dot-result')?.textContent.includes('gateway literal'));
    assert.equal(cancelled,1); assert.equal(posts.length,0);
   assert.deepEqual(errors,[]);await context.close();
  }
 });
}
