const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const {test} = require('node:test');
const {chromium,webkit} = require('playwright');
const assets = process.env.MAGPIE_DOT_ASSETS || path.resolve(__dirname,'../assets');
for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ['chromium','webkit']) {
 test(`${engine}: Dot task gate, stable recovery, literal results and local pairing`, async t => {
  const browser = await (engine==='webkit'?webkit.launch():chromium.launch({channel:'chromium'}));
  t.after(()=>browser.close());
  for(const lang of ['en','zh','zh-TW','ja','de']) for(const width of [1100,420]) {
   const context=await browser.newContext({viewport:{width,height:740},reducedMotion:'reduce'});
   const page=await context.newPage();page.setDefaultTimeout(7000);
   let available=false, posts=[], gets=[], identity=null, cancelled=0;
   const errors=[];page.on('pageerror',e=>errors.push(e.message));
   await page.route('**/*',async route=>{
    const r=route.request(), u=new URL(r.url());const json=data=>route.fulfill({json:data});
    if(u.pathname==='/boot.js')return route.fulfill({contentType:'text/javascript',body:`window.bootPrefs={lang:${JSON.stringify(lang)},theme:'light'};`});
    if(u.pathname==='/wails/runtime.js')return route.fulfill({contentType:'text/javascript',body:'export const Window={};'});
    if(u.pathname==='/api/state')return json({agents:[],profiles:[],settings:{lang,theme:'light'}});
    if(u.pathname==='/api/providers')return json({providers:[],presets:[],gateway:{running:true,url:'http://127.0.0.1:3999',calls:[],groups:[]}});
    if(u.pathname==='/api/dot/status')return json({enabled:true,code:available?'ready':'unverified',task_available:available,capabilities:{tasks:available}});
    if(u.pathname==='/api/dot/requests')return json(identity?[{request_id:identity.request_id,session_id:identity.session_id,mode:'task',digest:'a'.repeat(64),state:'acceptance_unknown',block_replay:true}]:[]);
    if(u.pathname==='/api/dot/jobs'){posts.push(r.postDataJSON());identity=posts[0];return route.fulfill({status:409,json:{code:'outcome_unknown'}});}
    if(u.pathname.startsWith('/api/dot/requests/')){
     gets.push(u.pathname);
     if(u.pathname.endsWith('/cancel')){cancelled++;return json({code:'cancel_intent_acknowledged'});}
     return json({version:1,request_digest:'a'.repeat(64),request_id:identity.request_id,session_id:identity.session_id,mode:'task',job_id:'job-fixture',state:'completed',result:{text:'<img src=x onerror="window.pwned=1">',artifacts:[{url:'javascript:alert(1)',label:'unsafe'},{url:'https://artifact.invalid/report',label:'Report'}]}});
    }
    if(u.pathname==='/api/dot/off'){available=false;return json({enabled:false});}
    if(u.pathname.startsWith('/api/'))return json({});
    if(u.host!=='magpie.test')return route.fulfill({status:404,body:''});
    const file=path.join(assets,u.pathname==='/'?'index.html':u.pathname);
    try{return route.fulfill({body:await fs.readFile(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]});}catch{return route.fulfill({status:404,body:''});}
   });
   await page.goto('http://magpie.test/?view=dot');
   const submit=page.locator('#dot-submit');await submit.waitFor();
   await page.locator('#dot-task').fill('Summarize explicit data');
   assert.equal(await submit.isDisabled(),true);assert.equal(posts.length,0);
   assert.equal(await page.locator('#view-dot input[type=file]').count(),0);
   assert.ok((await page.locator('#dot-pair').textContent()).length>10);
   if(lang!=='en')assert.notEqual(await submit.textContent(),'Submit background task');
   available=true;await page.locator('#dot-refresh').click();await page.waitForFunction(()=>!document.querySelector('#dot-submit').disabled);
   await page.locator('#dot-context').fill('Only selected text');
   const before=await page.evaluate(()=>scrollY);await submit.focus();await page.keyboard.press('Enter');
   await page.waitForFunction(()=>!document.querySelector('#dot-lookup').disabled);
   assert.equal(posts.length,1);assert.equal(posts[0].messages[0].content,'Only selected text');
   assert.equal(await submit.isDisabled(),true);
   await page.locator('#dot-history button').last().waitFor();
   await page.reload(); await page.locator('#dot-lookup').waitFor();
   assert.equal(await page.locator('#dot-submit').isDisabled(),true);
   assert.ok((await page.locator('#dot-identity').textContent()).includes(posts[0].request_id));
   await page.locator('#dot-lookup').click();await page.locator('#dot-result pre').waitFor();
   assert.equal(await page.locator('#dot-result pre').textContent(),'<img src=x onerror="window.pwned=1">');
   assert.equal(await page.locator('#dot-result img').count(),0);assert.equal(await page.locator('#dot-result a').count(),1);
   assert.equal(posts.length,1);assert.ok(gets.some(p=>p.endsWith(posts[0].request_id)));
   await page.locator('#dot-history button').last().focus(); await page.keyboard.press('Enter');
   await page.waitForFunction(()=>document.querySelector('#dot-notice').textContent===t('Cancellation is intent only; refresh to inspect completion')); assert.equal(cancelled,1);
   assert.equal(await page.evaluate(()=>window.pwned),undefined);
   assert.equal(await page.evaluate(()=>scrollY),before);
   assert.ok(await page.locator('#view-dot').evaluate(n=>n.scrollWidth<=n.clientWidth+1));
   assert.deepEqual(errors,[]);await context.close();
  }
 });
}
