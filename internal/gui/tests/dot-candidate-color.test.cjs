const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const {test} = require('node:test');
const {chromium, webkit} = require('playwright');
const assets = path.resolve(__dirname, '../assets');
for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ['chromium','webkit']) {
 for (const lang of ['en','zh','zh-TW','ja','de']) test(`${engine} ${lang}: ready omitted Dot keeps candidate color; unavailable Dot dims with reason`, async t => {
  const browser = await (engine === 'webkit' ? webkit.launch() : chromium.launch({channel:'chromium'}));
  t.after(() => browser.close());
  const page = await browser.newPage({viewport:{width:1100,height:900}});
  page.setDefaultTimeout(10000);
  let code = 'ready', posts = 0;
  const errors = []; page.on('pageerror', e => errors.push(e.message));
  const time = new Date().toISOString();
  const account = {id:'codex',provider:'codex',name:'Github',kind:'account',model:'gpt-6.1-sol'};
  const trace = {id:1,seq:1,time,agent:'opencode',model:'group/github',provider:'group/github',group:{id:'github',name:'Github',members:['codex/gpt-6.1-sol','dot/text','dot/task'],subs:[]},order:[account],tries:[{id:'codex',model:account.model,start:time,done:false}],done:false,display_skipped_dot:[{model:'text',mode:'answer',reason:'caller_tools'},{model:'task',mode:'task',reason:'caller_tools'}]};
  await page.route('**/*', async route => {
   const req = route.request(), u = new URL(req.url()), json = data => route.fulfill({json:data});
   if (req.method() === 'POST' && u.pathname.startsWith('/api/dot/')) posts++;
   if (u.pathname === '/boot.js') return route.fulfill({contentType:'text/javascript',body:`window.bootPrefs={lang:${JSON.stringify(lang)},theme:'light',featuresOff:['dot']};`});
   if (u.pathname === '/wails/runtime.js') return route.fulfill({contentType:'text/javascript',body:'export const Window={};'});
   if (u.pathname === '/api/state') return json({agents:[],profiles:[],settings:{lang,theme:'light'}});
   if (u.pathname === '/api/providers') return json({providers:[],presets:[],excluded:[],gateway:{running:true,window:true,url:'http://127.0.0.1:3999'}});
   if (u.pathname === '/api/groups') return json({models:[],groups:[],pools:[]});
   if (u.pathname === '/api/dot/status') return json({enabled:true,code,text_available:code==='ready',task_available:code==='ready'});
   if (u.pathname === '/api/gateway/trace') {
    if (u.searchParams.has('wait')) return new Promise(()=>{});
    return json({mine:true,seq:1,now:new Date().toISOString(),routes:[trace],totals:{}});
   }
   if (u.pathname.startsWith('/api/')) return json({});
   const file = path.join(assets, u.pathname === '/' ? 'index.html' : u.pathname);
   try { return route.fulfill({body:await fs.readFile(file),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(file)]}); }
   catch { return route.fulfill({status:404,body:''}); }
  });
  await page.goto('http://magpie.test/?view=routing');
  const row = page.locator('.rt-accts li').filter({hasText:'DotControl'});
  await row.waitFor();
  const note = await page.evaluate(()=>t('Not involved in this execution: {reason}',{reason:t('{reason} ({modes})',{reason:t('Dot skipped caller tools'),modes:['text','task'].join(t(', '))})}));
  await page.waitForFunction(note=>[...document.querySelectorAll('.rt-accts li')].some(n=>n.textContent.includes(note)),note);
  await page.waitForFunction(()=>document.querySelector('#rt-dot-status').dataset.state==='ready');
  const appearance = () => row.evaluate(n=>({dimmed:n.classList.contains('left'),opacity:getComputedStyle(n.querySelector('b')).opacity}));
  assert.deepEqual(await appearance(),{dimmed:false,opacity:'1'},'ready omitted Dot must keep normal candidate color');
  const labels = {unpaired:'Dot is not paired; pair in the terminal',offline:'DotControl is offline',probe_required:'Dot needs a capability probe in DotControl',subscription_expired:'Dot subscription expired; reconnect ChatGPT',disabled:'Dot is disabled locally',access_revoked:'Dot access was revoked; pair again',bridge_paused:'Dot bridge is paused'};
  for (const [state,label] of Object.entries(labels)) {
   code = state;
   await page.locator('#rt-dot-status button').first().click();
   const reason = await page.evaluate(label=>t(label),label);
   await page.waitForFunction(reason=>[...document.querySelectorAll('.rt-accts li')].some(n=>n.textContent.includes('DotControl')&&n.textContent.includes(reason)),reason);
   assert.deepEqual(await appearance(),{dimmed:true,opacity:'0.45'},state+' Dot dims');
   assert.ok((await row.textContent()).includes(note),'status must preserve request omission evidence');
  }
  code = 'ready';
  await page.locator('#rt-dot-status button').first().click();
  await page.waitForFunction(()=>[...document.querySelectorAll('.rt-accts li')].some(n=>n.textContent.includes('DotControl')&&!n.classList.contains('left')));
  assert.deepEqual(await appearance(),{dimmed:false,opacity:'1'});
  assert.equal(await row.count(),1);
  assert.equal(await page.locator('.rt-accts li.on, .rt-accts li.now').filter({hasText:'DotControl'}).count(),0);
  assert.equal(posts,0); assert.deepEqual(errors,[]);
 });
}
