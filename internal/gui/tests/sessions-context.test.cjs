// Fixture-only context handoff: run with Node's test runner and Playwright.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const { test } = require('node:test');
const { chromium, webkit } = require('playwright');
const { reach } = require('./reader.cjs');
const assets = path.resolve(__dirname, '../assets');
const excerpt = '<img src=x onerror=alert(1)> literal excerpt';
const at = new Date(Date.now() - 3600000).toISOString();
const sess = (id, extra = {}) => ({ agent: 'claude', id, title: 'Chat ' + id, cwd: '/work/app', source: 'localhost', last: at, start: at, models: [], transcript: true, deletable: true, size: 123, ...extra });
function serve(lang, calls, control) {
  const sessions = [sess('local'), sess('gateway', { gateway: true, source: '', parent_session: 'local' }), sess('readonly', { read_only: true, deletable: false }), sess('unreadable', { transcript: false }), sess('wsl', { wsl: 'Ubuntu' }), sess('source', { source: 'fixture-source' }), sess('child', { parent_session: 'local', cwd: '/work/child' }), sess('gateway-empty', { gateway: true, cwd: '', source: '' }), ...Array.from({ length: 20 }, (_, i) => sess('extra-' + i))];
  return async route => {
    const url = new URL(route.request().url());
    const json = data => route.fulfill({ json: data });
    if (url.pathname === '/boot.js') return route.fulfill({ contentType: 'text/javascript', body: `window.bootPrefs={lang:${JSON.stringify(lang)},theme:'light',web:false};` });
    if (url.pathname === '/wails/runtime.js') return route.fulfill({ contentType: 'text/javascript', body: 'export const Window = {};' });
    if (url.pathname === '/api/state') return json({ agents: [], profiles: [], settings: { lang, theme: 'light' } });
    if (url.pathname === '/api/providers') return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
    if (url.pathname === '/api/plugins') return json({ plugins: [] });
    if (url.pathname === '/api/groups') return json({ groups: [] });
    if (url.pathname === '/api/sessions/manage') return json({ agents: [{ agent: 'claude', count: sessions.length, deletable: true, name: 'Claude Code', icon: 'claude' }, { agent: 'opencode', count: 1, name: 'OpenCode', icon: 'opencode' }], agent: url.searchParams.get('agent') === 'opencode' ? 'opencode' : 'claude', sessions: url.searchParams.get('agent') === 'opencode' ? [sess('other', { agent: 'opencode' })] : sessions.filter(s => s.id !== control.removed && !(control.deleted || []).includes(s.id)), trash: [], trashDir: '~/trash' });
    if (url.pathname === '/api/sessions/context_export') {
      const refs = route.request().postDataJSON();
      calls.push({ refs, method: route.request().method() });
      const mode = control.mode;
      if (control.hold) await control.hold;
      if (mode === 'error') return route.fulfill({ status: 500, json: { error: 'fixture failure' } });
      return json({ text: mode === 'all' ? 'Protocol header only' : excerpt, sessions: refs.map((key, i) => ({ key, title: key, source: 'localhost', cut: !i, ...(mode === 'all' || i ? { error: i ? 'unavailable' : 'no_text' } : {}) })), omitted: true });
    }
    if (url.pathname === '/api/copy') { calls.push({ copy: route.request().postDataJSON() }); return control.copyFail ? route.fulfill({ status: 501, body: '' }) : json({}); }
    if (url.pathname === '/api/sessions/delete') {
      const ids = route.request().postDataJSON().ids;
      calls.push({ deleted: ids });
      control.deleted = ids;
      return json({ deleted: ids, refused: [] });
    }
    if (url.pathname.startsWith('/api/')) return json({});
    const file = path.join(assets, url.pathname === '/' ? 'index.html' : url.pathname);
    const body = await fs.readFile(file).catch(() => null);
    return route.fulfill({ status: body ? 200 : 404, body: body || '', contentType: { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.svg': 'image/svg+xml' }[path.extname(file)] });
  };
}
const nav = { en: 'Sessions', zh: '会话', 'zh-TW': '會話', ja: 'セッション', de: 'Sitzungen' };
for (const engine of process.env.BROWSER ? [process.env.BROWSER] : ['chromium', 'webkit']) for (const lang of Object.keys(nav)) {
  test(`${engine} ${lang}: independent context selection, literal preview and explicit copy`, async t => {
    const browser = await (engine === 'webkit' ? webkit.launch() : chromium.launch({ channel: 'chromium' }));
    t.after(() => browser.close());
    const page = await browser.newPage({ viewport: { width: 560, height: 800 }, reducedMotion: 'reduce' });
    page.setDefaultTimeout(5000);
    const calls = [], control = {}, errors = [];
    page.on('pageerror', e => errors.push(e.message));
    await page.route('**/*', serve(lang, calls, control));
    await page.goto('http://magpie.test/?view=providers');
    await page.locator('#nav').getByRole('button', { name: nav[lang], exact: true }).click();
    const view = page.locator('#view-sessions');
    const row = id => view.locator(`.sm-sess[data-id="${id}"]`);
    await row('local').waitFor();
    const missingRetry = await page.evaluate(() => ['zh', 'zh-TW', 'ja', 'de'].filter(l => !I18N[l]['Retry context preparation']));
    assert.deepEqual(missingRetry, [], 'Retry is translated in every shipped language');
    assert.equal(await row('local').locator('.sm-context-check').count(), 1, 'readable rows offer a separate context checkbox');
    assert.equal(await row('unreadable').locator('.sm-context-check').count(), 0);
    assert.equal(await row('readonly').locator('.sm-context-check').count(), 1);
    assert.equal(await view.locator('.sm-group').count(), 5, 'equal folders from gateway, WSL, source and localhost remain separate; cross-category children stay on top');
    const localFolder = row('local').locator('xpath=ancestor::div[contains(@class,"sm-group")]');
    await localFolder.locator('.sm-folder-check').check();
    await reach(page, row('local').locator('xpath=..').locator('.sm-subs-fold'));
    await row('local').locator('xpath=..').locator('.sm-subs-fold').click();
    assert.equal(await row('child').locator('.sm-check').isChecked(), false, 'folder selection excludes a nested child from another cwd');
    await localFolder.locator('.sm-folder-check').uncheck();
    await reach(page, row('local').locator('xpath=..').locator('.sm-subs-fold'));
    await row('local').locator('xpath=..').locator('.sm-subs-fold').click();
    assert((await view.locator('.sm-source').allTextContents()).some(s => s.includes('Ubuntu')));
    assert((await view.locator('.sm-source').allTextContents()).some(s => s.includes('fixture-source')));
    // Gateway starts folded, so find it by its provenance label.
    const gatewayFolders = page.locator('.sm-group').filter({ has: page.locator('.sm-source-gateway') });
    const gatewayFolder = gatewayFolders.first();
    const emptyGateway = gatewayFolders.last();
    assert.equal(await emptyGateway.locator('.sm-folder .sm-source').count(), 0, 'gateway provenance is not repeated when it already names the folder');
    const relocateLabel = await page.evaluate(() => t('Relocate project'));
    assert.equal(await localFolder.getByRole('button', { name: relocateLabel, exact: true }).count(), 0, 'mixed WSL/native paths keep the existing relocation restriction');
    await reach(page, gatewayFolder.locator('.fold').first());
    await gatewayFolder.locator('.fold').first().click();
    await reach(page, row('gateway').locator('.sm-context-check'));
    await row('gateway').locator('.sm-context-check').check();
    await reach(page, row('local').locator('.sm-check:not(.sm-context-check)'));
    await row('local').locator('.sm-check:not(.sm-context-check)').check();
    const top = await row('local').evaluate(e => e.getBoundingClientRect().top);
    await row('local').locator('.sm-context-check').check();
    assert.equal(await row('local').evaluate(e => e.getBoundingClientRect().top), top, 'checkbox does not move the page');
    assert.equal(await view.locator('.sess-detail').count(), 0);
    await row('local').locator('.sm-check:not(.sm-context-check)').uncheck();
    assert.equal(await row('local').locator('.sm-context-check').isChecked(), true);
    const prepare = view.locator('.sm-context-prepare');
    await reach(page, prepare);
    await prepare.click();
    const modal = page.locator('#modal');
    await modal.locator('.sm-context-preview').waitFor();
    assert.deepEqual(calls[0], { method: 'POST', refs: ['claude:gateway', 'claude:local'] });
    assert.equal(await modal.locator('.sm-context-preview').textContent(), excerpt);
    assert.equal(await modal.locator('.sm-context-preview img').count(), 0);
    assert.equal(calls.filter(c => c.copy).length, 0, 'prepare never copies');
    assert.equal(await modal.locator('.sm-context-result').count(), 2);
    await modal.locator('.sm-context-copy').click();
    await page.waitForFunction(() => document.querySelector('#status').classList.contains('ok'));
    assert.equal(calls.filter(c => c.copy).length, 1);
    assert(JSON.stringify(calls.find(c => c.copy).copy).includes(excerpt));
    await page.keyboard.press('Escape');
    await modal.waitFor({ state: 'hidden' });
    assert.equal(await prepare.evaluate(e => document.activeElement === e), true, 'Escape returns focus');
    control.mode = 'all';
    await prepare.click();
    await modal.locator('.sm-context-preview').waitFor();
    assert.equal(await modal.locator('.sm-context-copy').isDisabled(), true, 'headers are not eligible text when all entries fail');
    await modal.locator('.sm-context-close').click();
    await modal.waitFor({ state: 'hidden' });
    assert.equal(await row('local').locator('.sm-context-check').isChecked(), true);
    assert.equal(await row('gateway').locator('.sm-context-check').isChecked(), true);
    // Late responses are invalidated immediately by closing the dialog.
    control.mode = '';
    let release;
    control.hold = new Promise(r => { release = r; });
    await prepare.click();
    await page.waitForFunction(() => !!document.querySelector('.sm-context-loading'));
    await modal.locator('.sm-context-close').click();
    release(); control.hold = null;
    await modal.waitFor({ state: 'hidden' });
    assert.equal(calls.filter(c => c.copy).length, 1);
    // An in-flight preview is retired on selection change, even if its
    // response has already been queued by the server.
    control.hold = new Promise(r => { release = r; });
    await prepare.click();
    await modal.locator('.sm-context-loading').waitFor();
    await row('local').locator('.sm-context-check').evaluate(c => { c.checked = false; c.dispatchEvent(new Event('change')); });
    release(); control.hold = null;
    await modal.waitFor({ state: 'hidden' });
    await row('local').locator('.sm-context-check').check();
    control.mode = 'error';
    await prepare.click();
    await modal.locator('.sm-context-retry:not([hidden])').waitFor();
    assert.equal(await modal.locator('.sm-context-copy').isDisabled(), true);
    control.mode = '';
    await modal.locator('.sm-context-retry').click();
    await modal.locator('.sm-context-preview').waitFor();
    assert.equal(await modal.locator('.sm-context-copy').isEnabled(), true);
    control.copyFail = true;
    await page.evaluate(() => {
      Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async () => { throw new Error('fixture clipboard failure'); } } });
      document.execCommand = () => false;
    });
    await modal.locator('.sm-context-copy').click();
    await page.waitForFunction(() => document.querySelector('#status').classList.contains('err'));
    assert.equal(await modal.locator('.sm-context-copy').isEnabled(), true, 'copy failure is reported by the real helper and stays retryable');
    control.copyFail = false;
    await page.keyboard.press('Escape');
    await modal.waitFor({ state: 'hidden' });
    await reach(page, row('local').locator('.sm-check:not(.sm-context-check)'));
    await row('local').locator('.sm-check:not(.sm-context-check)').check();
    await reach(page, view.locator('.sm-context-clear'));
    await view.locator('.sm-context-clear').click();
    assert.equal(await row('local').locator('.sm-context-check').isChecked(), false);
    assert.equal(await row('local').locator('.sm-check:not(.sm-context-check)').isChecked(), true, 'clearing context leaves deletion selection alone');
    await reach(page, row('local').locator('.sm-context-check'));
    await row('local').locator('.sm-context-check').check();
    await reach(page, row('local').locator('.sm-check:not(.sm-context-check)'));
    await row('local').locator('.sm-check:not(.sm-context-check)').uncheck();
    await reach(page, row('unreadable').locator('.sm-check:not(.sm-context-check)'));
    await row('unreadable').locator('.sm-check:not(.sm-context-check)').check();
    await reach(page, view.locator('.sm-delete'));
    await view.locator('.sm-delete').click();
    await modal.locator('.primary').click();
    await modal.waitFor({ state: 'hidden' });
    await page.waitForFunction(() => !document.querySelector('.sm-delete') || document.querySelector('.sm-delete').disabled);
    await row('local').waitFor();
    assert.equal(await row('local').locator('.sm-context-check').isChecked(), true, 'deleting another session leaves context selection alone');
    assert.deepEqual(calls.find(c => c.deleted).deleted, ['unreadable']);
    await row('unreadable').waitFor({ state: 'detached' });
    await view.locator('.sm-context-clear').click();
    // Cap includes hidden selections and refuses the 21st without replacement.
    for (const id of ['local', 'readonly', ...Array.from({ length: 18 }, (_, i) => 'extra-' + i)]) {
      const c = row(id).locator('.sm-context-check'); await reach(page, c); await c.check();
    }
    const beyond = row('extra-18').locator('.sm-context-check'); await reach(page, beyond); await beyond.click();
    assert.equal(await beyond.isChecked(), false);
    const limitText = await page.evaluate(() => t('Select at most 20 sessions for context.'));
    assert.equal(await page.locator('#status').textContent(), limitText, 'refusing the 21st selection announces the localized limit');
    assert.equal(await page.locator('#status').getAttribute('role'), 'alert', 'the shared status helper announces refusal as an accessible error');
    assert.equal(await view.locator('.sm-context-check:checked').count(), 20);
    assert((await view.locator('.sm-context-count').textContent()).includes('20'));
    assert.equal(await view.locator('.sm-context-bar').evaluate(e => e.getBoundingClientRect().right <= innerWidth), true);
    assert.equal(await row('local').locator('.sm-context-check').getAttribute('aria-describedby'), 'sm-context-limit');
    assert.equal(await page.evaluate(() => document.querySelectorAll('#sm-context-limit').length), 1, 'the limit note each checkbox describes is in the page once');
    await row('local').locator('.sm-context-check').evaluate(c => { c.checked = false; c.dispatchEvent(new Event('change')); });
    assert((await view.locator('.sm-context-count').textContent()).includes('19/20'), 'explicit deselection leaves 19 before the reload');
    // Reload retires a pending preview and drops only entries no longer listed.
    await reach(page, prepare);
    control.hold = new Promise(r => { release = r; });
    await prepare.click();
    await modal.locator('.sm-context-loading').waitFor();
    control.removed = 'readonly';
    await page.evaluate(() => show('sessions'));
    release(); control.hold = null;
    await modal.waitFor({ state: 'hidden' });
    await row('readonly').waitFor({ state: 'detached' });
    assert((await view.locator('.sm-context-count').textContent()).includes('18/20'), 'the completed reload prunes the removed selection (19 before, 18 after)');
    assert.equal(await view.locator('.sm-context-check:checked').count(), 18);
    // Filtering preserves hidden picks; an agent change clears them.
    await reach(page, view.locator('.sm-filter'));
    await view.locator('.sm-filter').fill('Chat local');
    assert((await view.locator('.sm-context-count').textContent()).includes('18/20'));
    await view.locator('.sm-filter').fill('');
    // Agent changes also invalidate a response that is still in flight.
    await view.locator('.sm-context-prepare').click();
    await modal.locator('.sm-context-preview').waitFor();
    await page.keyboard.press('Escape');
    await modal.waitFor({ state: 'hidden' });
    control.hold = new Promise(r => { release = r; });
    await view.locator('.sm-context-prepare').click();
    await modal.locator('.sm-context-loading').waitFor();
    await view.locator('.sm-agents .opt', { hasText: 'OpenCode' }).evaluate(b => b.click());
    release(); control.hold = null;
    await modal.waitFor({ state: 'hidden' });
    await row('other').waitFor();
    assert((await view.locator('.sm-context-count').textContent()).includes('0/20'));
    await row('other').locator('.sm-context-check').focus();
    await page.keyboard.press('Space');
    assert.equal(await row('other').locator('.sm-context-check').isChecked(), true, 'context selection supports the keyboard');
    await view.locator('.sm-trash-btn').click();
    await view.locator('.sm-trash-btn').click();
    assert((await view.locator('.sm-context-count').textContent()).includes('0/20'), 'opening trash clears context');
    await view.locator('.sm-agents .opt', { hasText: 'Claude Code' }).click();
    await row('local').waitFor();
    // Usage's input can omit source; the loaded session opens its own group.
    await page.evaluate(() => magpie.call('sessions', 'reveal', { agent: 'claude', id: 'source', cwd: '/work/app' }));
    await row('source').waitFor();
    assert.equal(await row('source').locator('xpath=..').evaluate(e => e.classList.contains('open')), true);
    // Plugin-local keys are available in every shipped translation.
    const missing = await page.evaluate(() => ['Retry context preparation', 'Prepare context', 'Copy context', 'Select context: {title}', '{n}/20 context sessions', 'No eligible text', 'Local files', 'WSL on this computer: {name}']
      .filter(k => ['zh', 'zh-TW', 'ja', 'de'].some(l => !I18N[l][k])));
    assert.deepEqual(missing, []);
    assert.deepEqual(errors, []);
  });
}
