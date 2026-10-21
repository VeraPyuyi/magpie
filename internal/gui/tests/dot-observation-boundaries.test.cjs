const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const {test} = require('node:test');
const {chromium, webkit} = require('playwright');
const assets = path.resolve(__dirname, '../assets');
const deferred = () => { let resolve; const promise = new Promise(r => resolve=r); return {promise,resolve}; };
for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ['chromium','webkit']) {
 test(`${engine}: selected task B rejects held task A detail after poll ticks; replay hides completed state before observed_at (five locales)`, async t => {
  const browser = await (engine==='webkit' ? webkit.launch() : chromium.launch({channel:'chromium'}));
  t.after(()=>browser.close());
  for (const lang of ['en','zh','zh-TW','ja','de']) {
   const page = await browser.newPage({viewport:{width:1100,height:900}}); page.setDefaultTimeout(15000);
   const now=Date.parse('2026-10-21T12:00:00Z'), start=now-20000, iso=n=>new Date(n).toISOString();
   await page.clock.install({time:now}); await page.clock.pauseAt(now);
   const held=deferred(), entered=deferred(); let getsB=0, posts=0;
   const make=(id,n)=>({id:n,seq:n,time:iso(start),agent:'opencode',model:'dot/task',provider:'dot',order:[{id:'dot',provider:'dot',name:'DotControl',kind:'provider',model:'task'}],tries:[{id:'dot',model:'task',start:iso(start),ms:10000,done:true,status:200,dot:{request_id:id,state:'accepted'},dotJob:{request_id:id,job_id:'job-'+id,mode:'task',state:id==='B'?'completed':'running',observed_at:iso(start+8000)}}],done:true,status:200,ms:10000,display_skipped_dot:[{model:'text',mode:'answer',reason:'caller_tools',observed_at:iso(start+5000)}]});
   // A is the newest request and starts an automatic detail GET. B is selected
   // through the actual request list while A's response is held at the network.
   await page.route('**/*',async route=>{
    const req=route.request(),u=new URL(req.url()),json=data=>route.fulfill({json:data});
    if(req.method()==='POST'&&u.pathname.startsWith('/api/dot/'))posts++;
    if(u.pathname==='/boot.js')return route.fulfill({contentType:'text/javascript',body:`window.bootPrefs={lang:${JSON.stringify(lang)},theme:'light',featuresOff:['dot']};`});
    if(u.pathname==='/wails/runtime.js')return route.fulfill({contentType:'text/javascript',body:'export const Window={};'});
    if(u.pathname==='/api/state')return json({agents:[],profiles:[],settings:{lang,theme:'light'}});
    if(u.pathname==='/api/providers')return json({providers:[],excluded:[],presets:[],gateway:{running:true,window:true,url:'http://127.0.0.1:3999'}});
    if(u.pathname==='/api/groups')return json({models:[],groups:[],pools:[]});
    if(u.pathname==='/api/dot/status')return json({enabled:true,code:'ready'});
    if(u.pathname==='/api/dot/gateway/requests/A'){entered.resolve();await held.promise;return json({request_id:'A',job_id:'job-A',state:'completed',summary:'LATE_A_SENTINEL',result:{text:'A_RESULT_SENTINEL'}}).catch(()=>{});}
    if(u.pathname==='/api/dot/gateway/requests/B'){getsB++;return json({request_id:'B',job_id:'job-B',state:'completed',result:{text:'B_RESULT_SENTINEL'}});}
    if(u.pathname==='/api/gateway/trace'){
     if(u.searchParams.has('wait'))return new Promise(()=>{});
     return json({mine:true,seq:2,now:iso(now),routes:[make('B',1),make('A',2)],totals:{}});
    }
    if(u.pathname.startsWith('/api/'))return json({});
    try {return route.fulfill({body:await fs.readFile(path.join(assets,u.pathname==='/'?'index.html':u.pathname)),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(u.pathname==='/'?'index.html':u.pathname)]});}catch{return route.fulfill({status:404,body:''});}
   });
   await page.goto('http://magpie.test/?view=routing'); await page.clock.runFor(1000); await entered.promise;
   const rows=page.locator('.rt-reqs button.rt-req'); assert.equal(await rows.count(),2);
   // Request rows are newest first, so the second row is B.
   await rows.nth(1).focus();await page.keyboard.press('Enter');await page.clock.runFor(50);
   assert.ok((await page.locator('.rt-dot-job').textContent()).includes('job-B'));
   await page.clock.runFor(6000);
   const responseDone=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/dot/gateway/requests/A');
   held.resolve(); await (await responseDone).finished();
   await page.clock.runFor(100);
   // Explicit refresh is a synchronization barrier after A's network completion.
   await page.locator('.rt-dot-job button').focus(); await page.keyboard.press('Enter');
   await page.waitForFunction(()=>document.querySelector('.rt-dot-result')?.textContent==='B_RESULT_SENTINEL');
   await page.clock.runFor(6000);
   const card=await page.locator('.rt-dot-job').textContent();
   assert.ok(card.includes('job-B')&&card.includes('B_RESULT_SENTINEL'));assert.ok(!card.includes('A_RESULT_SENTINEL')&&!card.includes('LATE_A_SENTINEL'));assert.ok(getsB>=1);assert.equal(posts,0);
   // Replay B's 10-second route: observation at +8s. Seek actual replay DOM
   // to +2s (strictly before), then +9s (strictly after), without wall sleeps.
   const replay=page.locator('.rt-log-head').getByRole('button',{name:await page.evaluate(()=>t('Replay')),exact:true});
   await replay.focus();await page.keyboard.press('Enter');await page.clock.runFor(20);
   const seek=async fraction=>{await page.locator('.rp-track').evaluate((node,f)=>{const b=node.getBoundingClientRect();node.dispatchEvent(new MouseEvent('click',{bubbles:true,clientX:b.left+b.width*f}));},fraction);await page.clock.runFor(20);};
   await seek(.2);
   assert.equal(await page.locator('.rt-dot-job:visible').count(),0,'before +8s, replay must not inherit completed Dot job or later GET result');
   assert.equal(await page.locator('.rt-dot-result:visible').count(),0);
   await seek(.49);
   assert.equal(await page.locator('.rt-accts li.left').count(),0);
   await page.clock.runFor(400);
   assert.equal(await page.locator('.rt-accts li.left').count(),1,'skip must appear crossing +5s without try/count/done changes');
   await seek(.9);
   await page.waitForFunction(()=>document.querySelector('.rt-dot-job:not([hidden])')?.textContent.includes('job-B'));
   assert.ok((await page.locator('.rt-dot-state').textContent()).includes(await page.evaluate(()=>t('Dot state: completed'))));
   assert.ok(!(await page.locator('.rt-dot-job').textContent()).includes('B_RESULT_SENTINEL'),'later recovery result must not enter recorded replay');
   await page.close();
  }
 });
}
