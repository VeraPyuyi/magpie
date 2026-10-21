const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const {test} = require('node:test');
const {chromium,webkit} = require('playwright');
const assets=path.resolve(__dirname,'../assets');
for(const engine of process.env.BROWSER?[process.env.BROWSER]:['chromium','webkit']) test(`${engine}: Dot skipped seats and execution card in five locales`,async t=>{
 const browser=await(engine==='webkit'?webkit.launch():chromium.launch({channel:'chromium'}));t.after(()=>browser.close());
 for(const lang of ['en','zh','zh-TW','ja','de']) {
  const page=await browser.newPage({viewport:{width:560,height:900}});page.setDefaultTimeout(15000);
  let stage='skipped', state='running', gets=0, posts=0; const errors=[];page.on('pageerror',e=>errors.push(e.message));
  const time=new Date(Date.now()-10000).toISOString();
  const account={id:'codex',provider:'codex',name:'Github',kind:'account',who:'fixture',model:'gpt-6.1-sol'};
  const dotOrder={id:'dot',provider:'dot',name:'DotControl',kind:'provider',model:'task'};
  const dotTry={id:'dot',model:'task',start:time,done:true,status:200,dot:{request_id:'req-visible',state:'accepted'},dotJob:{request_id:'req-visible',job_id:'job-visible',mode:'task',state:'queued',observed_at:time}};
  function routeData(){
   const skippedDot=[{model:'text',mode:'answer',reason:'caller_tools'},{model:'task',mode:'task',reason:'caller_tools'}];
   const mixedDot=[{model:'text',mode:'answer',reason:'caller_tools'},{model:'task',mode:'task',reason:'task_prompt_required'}];
   if(stage==='skipped')return {id:1,seq:Date.now(),time,agent:'opencode',model:'group/github',provider:'group/github',group:{id:'github',name:'Github',members:['codex/gpt-6.1-sol','dot/text','dot/task'],subs:[]},order:[account,...Array.from({length:25},(_,i)=>({id:'vendor'+i,provider:'vendor'+i,name:'Vendor '+i,kind:'provider',model:'model'}))],tries:[{id:'codex',model:account.model,start:time,done:false}],done:false,status:200,display_skipped_dot:skippedDot};
   if(stage==='mixed')return {id:1,seq:Date.now(),time,agent:'opencode',model:'group/github',provider:'group/github',group:{id:'github',name:'Github',members:['codex/gpt-6.1-sol','dot/text','dot/task'],subs:[]},order:[dotOrder],tries:[dotTry],done:true,status:200,display_skipped_dot:mixedDot};
   return {id:1,seq:Date.now(),time,agent:'opencode',model:'group/github',provider:'group/github',group:{id:'github',name:'Github',members:['codex/gpt-6.1-sol','dot/text','dot/task'],subs:[]},order:[dotOrder],tries:[dotTry],done:true,status:200,display_skipped_dot:[]};
  }
  await page.route('**/*',async route=>{
   const req=route.request(),u=new URL(req.url()),json=data=>route.fulfill({json:data});
   if(req.method()==='POST' && u.pathname.startsWith('/api/dot/'))posts++;
   if(u.pathname==='/boot.js')return route.fulfill({contentType:'text/javascript',body:`window.bootPrefs={lang:${JSON.stringify(lang)},theme:'light',featuresOff:['dot']};`});
   if(u.pathname==='/wails/runtime.js')return route.fulfill({contentType:'text/javascript',body:'export const Window={};'});
   if(u.pathname==='/api/state')return json({agents:[],profiles:[],settings:{lang,theme:'light'}});
   if(u.pathname==='/api/providers')return json({providers:[],excluded:[],presets:[],gateway:{running:true,window:true,url:'http://127.0.0.1:3999'}});
   if(u.pathname==='/api/dot/status')return json({enabled:true,code:'ready',text_available:true,task_available:true});
   if(u.pathname==='/api/dot/gateway/requests/req-visible'){gets++;return json({request_id:'req-visible',job_id:'job-visible',mode:'task',state,summary:'<img src=x onerror=alert(1)>',result:state==='completed'?{text:'<script>window.bad=true</script>',artifacts:[{url:'https://example.org/artifact',label:'result'}]}:null});}
   if(u.pathname==='/api/groups')return json({models:[],groups:[],pools:[]});
   if(u.pathname==='/api/gateway/trace'){if(u.searchParams.has('wait')){await new Promise(r=>setTimeout(r,200));return json({mine:true,now:new Date().toISOString(),seq:1,routes:[],totals:{}}).catch(()=>{});}return json({mine:true,now:new Date().toISOString(),seq:1,routes:[routeData()],totals:{}});}
   if(u.pathname.startsWith('/api/'))return json({});
   try{return route.fulfill({body:await fs.readFile(path.join(assets,u.pathname==='/'?'index.html':u.pathname)),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(u.pathname==='/'?'index.html':u.pathname)]});}catch{return route.fulfill({status:404,body:''});}
  });
  await page.goto('http://magpie.test/?view=routing');
  const skipped=page.locator('.rt-accts li.left').filter({hasText:'DotControl'});
  await skipped.first().waitFor();
  assert.equal(await skipped.count(),1,`${lang}: same-reason skips collapse to one row`);
  const shared=await page.evaluate(()=>t('Not involved in this execution: {reason}',{reason:t('{reason} ({modes})',{reason:t('Dot skipped caller tools'),modes:['text','task'].join(t(', '))})}));
  assert.ok((await skipped.textContent()).includes(shared),`${lang}: shared reason lists both modes`);
  assert.ok((await page.locator('.rt-accts').textContent()).includes('gpt-6.1-sol'));
  assert.equal(posts,0);
  stage='mixed';await page.reload();
  await skipped.first().waitFor();
  assert.equal(await skipped.count(),1);
  const mixed=await page.evaluate(()=>t('Not involved in this execution: {reason}',{reason:[t('{mode} {reason}',{mode:'text',reason:t('Dot skipped caller tools')}),t('{mode} {reason}',{mode:'task',reason:t('A final user task is required')})].join(t('; '))}));
  assert.ok((await skipped.textContent()).includes(mixed),`${lang}: different reasons stay separate`);
  assert.equal(await page.locator('.rt-accts li').filter({hasText:'DotControl'}).count(),2);
  assert.equal(await page.locator('.rt-accts li.on:not(.left)').filter({hasText:'DotControl'}).count(),1);
  stage='job';await page.reload();
  await page.locator('.rt-dot-job').waitFor();
  await page.waitForFunction(()=>document.querySelector('.rt-dot-job')?.textContent.includes('job-visible'));
  await page.waitForFunction(()=>document.querySelector('.rt-dot-job')?.textContent.includes('<img'));
  assert.equal(await page.locator('.rt-dot-job img').count(),0);
  state='waiting_user';await page.locator('.rt-dot-job button').click();
  await page.waitForTimeout(150);
  state='completed';await page.locator('.rt-dot-job button').click();
  await page.waitForFunction(()=>document.querySelector('.rt-dot-result')?.textContent.includes('<script>'));
  assert.equal(await page.locator('.rt-dot-job script').count(),0);
  assert.equal(await page.locator('.rt-dot-job a').getAttribute('href'),'https://example.org/artifact');
  assert.ok(gets>=3);assert.equal(posts,0);assert.deepEqual(errors,[]);
  const terminalGets=gets; await page.waitForTimeout(5200); assert.equal(gets,terminalGets,'terminal task must stop polling');
  await page.locator('#nav button[data-view="agents"]').click();
  await page.waitForTimeout(5200); assert.equal(gets,terminalGets,'hidden Routing must stop polling');
  await page.close();
 }
});
