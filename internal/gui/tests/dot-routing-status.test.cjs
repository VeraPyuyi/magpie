const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const {test} = require('node:test');
const {chromium,webkit} = require('playwright');
const assets=path.resolve(__dirname,'../assets');
for(const engine of process.env.BROWSER?[process.env.BROWSER]:['chromium','webkit']) test(`${engine}: Dot routing configuration remains visible while unavailable in every locale`,async t=>{
 const browser=await(engine==='webkit'?webkit.launch():chromium.launch({channel:'chromium'}));t.after(()=>browser.close());
 for(const lang of ['en','zh','zh-TW','ja','de']) {
  const page=await browser.newPage({viewport:{width:1100,height:900}});page.setDefaultTimeout(7000);
  let code='unpaired',saved=null;const errors=[];page.on('pageerror',e=>errors.push(e.message));
  const groups=()=>({models:[{id:'dot/text',name:'Dot text',provider:'dot',providerName:'DotControl',icon:'dot',unavailable:true,reason:'Dot needs readiness before new requests'},{id:'dot/task',name:'Dot task',provider:'dot',providerName:'DotControl',icon:'dot',unavailable:true,reason:'Dot needs readiness before new requests'}],pools:[],groups:[],deciders:[],templates:[]});
  await page.route('**/*',async route=>{
   const r=route.request(),u=new URL(r.url()),json=data=>route.fulfill({json:data});
   if(u.pathname==='/boot.js')return route.fulfill({contentType:'text/javascript',body:`window.bootPrefs={lang:${JSON.stringify(lang)},theme:'light',featuresOff:['dot']};`});
   if(u.pathname==='/wails/runtime.js')return route.fulfill({contentType:'text/javascript',body:'export const Window={};'});
   if(u.pathname==='/api/state')return json({agents:[],profiles:[],settings:{lang,theme:'light'}});
   if(u.pathname==='/api/providers')return json({providers:[],presets:[],gateway:{running:false,calls:[],groups:[]}});
   if(u.pathname==='/api/dot/status')return json({enabled:true,code,text_available:code==='ready',subscription:{renewal_state:code==='ready'?'failed':'current'}});
   if(u.pathname==='/api/groups')return json(groups());
   if(u.pathname==='/api/groups/save'){saved=r.postDataJSON();return json({...groups(),groups:[{...saved,ready:false,memberInfo:[{id:'dot/task',ready:false}]}]});}
   if(u.pathname==='/api/gateway/trace') {if(u.searchParams.has('wait'))return new Promise(()=>{});return json({mine:false,now:new Date().toISOString(),seq:1,routes:[],totals:{}});}
   if(u.pathname.startsWith('/api/'))return json({});
   try{return route.fulfill({body:await fs.readFile(path.join(assets,u.pathname==='/'?'index.html':u.pathname)),contentType:{'.html':'text/html','.js':'text/javascript','.css':'text/css','.svg':'image/svg+xml'}[path.extname(u.pathname==='/'?'index.html':u.pathname)]});}catch{return route.fulfill({status:404,body:''});}
  });
  await page.goto('http://magpie.test/?view=routing');
  const row=page.locator('#rt-dot-status');await row.waitFor();
  for(const state of ['unpaired','disabled','offline','probe_required','subscription_expired','ready']) {
   code=state;await row.getByRole('button',{name:await page.evaluate(()=>t('Refresh Dot status')),exact:true}).click();await page.waitForFunction(s=>document.querySelector('#rt-dot-status').dataset.state===s,state);
   assert.ok((await row.textContent()).includes('Dot'));
  }
  await page.locator('#rtNewGroup').click();
  await page.locator('.rt-gadd').first().click();
  const option=page.locator('#pop').getByText('Dot task',{exact:true});await option.click();
  assert.ok((await page.locator('.rt-gedit').textContent()).includes('Dot'));
  await page.keyboard.press('Escape');
  await page.locator('.rt-gedit input').first().fill('Dot configured');
  await page.locator('.rt-gedit button.primary').click();
  await page.waitForFunction(()=>!document.querySelector('.rt-gedit'));
  assert.deepEqual(saved.members,['dot/task']);
  assert.deepEqual(errors,[]);await page.close();
 }
});
