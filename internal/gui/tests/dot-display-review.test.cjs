const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const {test} = require('node:test');
const {chromium, webkit} = require('playwright');
const assets = path.resolve(__dirname, '../assets');
const locales = ['en','zh','zh-TW','ja','de'];
async function fixture(page, lang, routes) {
 let posts=0;
 await page.route('**/*', async route => {
  const req=route.request(), u=new URL(req.url()), json=data=>route.fulfill({json:data});
  if(req.method()==='POST' && u.pathname.startsWith('/api/dot/'))posts++;
  if(u.pathname==='/boot.js')return route.fulfill({contentType:'text/javascript',body:`window.bootPrefs={lang:${JSON.stringify(lang)},theme:'light'};`});
  if(u.pathname==='/wails/runtime.js')return route.fulfill({contentType:'text/javascript',body:'export const Window={};'});
  if(u.pathname==='/api/state')return json({agents:[],profiles:[],settings:{lang,theme:'light'}});
  if(u.pathname==='/api/providers')return json({providers:[],presets:[],excluded:[],gateway:{running:true,window:true,url:'http://127.0.0.1:3999'}});
  if(u.pathname==='/api/groups')return json({models:[],groups:[],pools:[]});
  if(u.pathname==='/api/dot/status')return json({enabled:true,code:'ready'});
  if(u.pathname==='/api/dot/requests'||u.pathname==='/api/dot/gateway/requests')return json([]);
  if(u.pathname==='/api/gateway/trace') {
   if(u.searchParams.has('wait'))return new Promise(()=>{});
   return json({mine:true,seq:3,now:new Date().toISOString(),routes,totals:{}});
  }
  if(u.pathname.startsWith('/api/'))return json({});
  const file=path.join(assets,u.pathname==='/'?'index.html':u.pathname);
  try{return route.fulfill({body:await fs.readFile(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]});}catch{return route.fulfill({status:404,body:''});}
 });
 return ()=>posts;
}
for(const engine of process.env.BROWSER?[process.env.BROWSER]:['chromium','webkit']) {
 for(const lang of locales) test(`${engine} ${lang}: concurrent skipped evidence survives identical candidate sets and deduplicates across different sets`,async t=>{
  const browser=await(engine==='webkit'?webkit.launch():chromium.launch({channel:'chromium'}));t.after(()=>browser.close());
  const page=await browser.newPage({viewport:{width:1100,height:900}});
  const time=new Date().toISOString();
  const account={id:'codex',provider:'codex',name:'Github',kind:'account',who:'fixture',model:'gpt-6.1-sol'};
  const make=(id,skips,order=[account])=>({id,seq:id,time,agent:'opencode',model:'group/github',provider:'group/github',group:{id:'github',name:'Github',members:['codex/gpt-6.1-sol','dot/text','dot/task'],subs:[]},order,tries:[{id:'codex',model:account.model,start:time,done:false}],done:false,display_skipped_dot:skips});
  const caller=[{model:'text',mode:'answer',reason:'caller_tools'},{model:'task',mode:'task',reason:'caller_tools'}];
  const required=[{model:'text',mode:'answer',reason:'task_prompt_required'},{model:'task',mode:'task',reason:'task_prompt_required'}];
  const routes=[make(1,caller),make(2,required)];
  const posts=await fixture(page,lang,routes);
  await page.goto('http://magpie.test/?view=routing');
  const skipped=page.locator('.rt-accts li.left').filter({hasText:'DotControl'});
  await skipped.first().waitFor();
  assert.equal(await skipped.count(),2,'both concurrent requests must retain their different omissions');
  const words=await page.evaluate(()=>[t('Dot skipped caller tools'),t('A final user task is required')]);
  const summaries=await skipped.allTextContents();
  for(const word of words)assert.ok(summaries.some(s=>s.includes(word)),word);
  await skipped.evaluateAll(nodes=>nodes.forEach((n,i)=>n.dataset.reviewStable=String(i)));
  // Relanguage rebuilds the actual routing stage without replacing stable rows.
  await page.evaluate(()=>magpie.call('routing','relang'));
  assert.deepEqual(await skipped.allTextContents(),summaries);
  assert.deepEqual(await skipped.evaluateAll(nodes=>nodes.map(n=>n.dataset.reviewStable)),['0','1']);
  routes[1]=make(2,caller,[account,{id:'other',provider:'other',name:'Other',kind:'provider',model:'other'}]);
  await page.reload();await skipped.first().waitFor();
  assert.equal(await skipped.count(),1,'identical summaries from different candidate sets appear once');
  assert.ok((await skipped.textContent()).includes(words[0]));
  routes[1]=make(2,[...caller].reverse());
  await page.reload();await skipped.first().waitFor();
  assert.equal(await skipped.count(),1,'identical evidence deduplicates with matching candidate sets and reversed record order');
  assert.equal(await page.locator('.rt-accts li.left.on, .rt-accts li.left.now').count(),0,'omissions never execute');
  assert.equal(posts(),0);
 });
 test(`${engine}: mounted Dot history source buttons retranslate through all five locales`,async t=>{
  const browser=await(engine==='webkit'?webkit.launch():chromium.launch({channel:'chromium'}));t.after(()=>browser.close());
  const page=await browser.newPage();const posts=await fixture(page,'en',[]);
  await page.goto('http://magpie.test/?view=dot');await page.locator('#dot-refresh').waitFor();
  const sources=page.locator('#view-dot .dot-actions').last().locator('button');
  assert.equal(await sources.count(),2);
  await sources.first().evaluate(n=>n.dataset.reviewMounted='yes');
  for(const lang of ['zh','zh-TW','ja','de','en']) {
   await page.evaluate(lang=>{applyPrefs({lang,theme:'light'});magpie.call('dot','relang');},lang);
   const expected=await page.evaluate(()=>[t('Requests from this window'),t('Routed through Magpie')]);
   assert.deepEqual(await sources.allTextContents(),expected,`${lang}: both source buttons must translate after mount`);
   assert.equal(await sources.first().getAttribute('data-review-mounted'),'yes');
  }
  assert.equal(posts(),0);
 });
}
