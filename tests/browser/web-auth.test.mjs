import test from 'node:test';
import assert from 'node:assert/strict';
import { setup } from './driver.mjs';

test('HTTPS login receives a protected Cookie and submits original text', async t => {
  const h = await setup(t);
  const context = await h.context();
  const page = await h.page(context);
  await page.login();
  await page.ready();
  const cookies = (await h.cdp.send('Storage.getCookies', { browserContextId: context })).cookies;
  const session = cookies.find(cookie => cookie.name === '__Host-shiyi-session');
  assert.ok(session, 'Chrome accepted the real login Cookie');
  assert.equal(session.httpOnly, true);
  assert.equal(session.secure, true);
  assert.equal(session.sameSite, 'Strict');
  await page.submit('  私人原文\n保留空白  ');
  await page.wait(`document.querySelector('.msg-record')`);
  assert.equal(await page.evaluate(`document.querySelector('.msg-user .msg-text').textContent`), '  私人原文\n保留空白  ');
  assert.equal((await h.control('stats')).calls, 1);
});

test('closing and reopening Chrome preserves the valid Cookie but not private history', async t => {
  const h = await setup(t);
  // The default context is the isolated on-disk profile, not an incognito jar.
  const page = await h.page();
  await page.login(); await page.ready();
  await privateConversation(h, page);
  const before = (await h.cdp.send('Storage.getCookies')).cookies.find(cookie => cookie.name === '__Host-shiyi-session');
  assert.ok(before && !before.session && before.expires > Date.now() / 1000, 'login issued a persistent Cookie');
  const restart = await h.restartBrowser();
  assert.equal(restart.exitCode, 0, 'the original Chrome process actually exited cleanly');
  assert.notEqual(restart.pid, restart.previousPID, 'a new Chrome process owns the same temporary profile');
  const reopened = await h.page();
  await reopened.ready(); // No login or injected Cookie after relaunch.
  const after = (await h.cdp.send('Storage.getCookies')).cookies.find(cookie => cookie.name === '__Host-shiyi-session');
  assert.ok(after && after.value === before.value, 'Chrome retained the original login Cookie');
  await assertFreshPrivateView(h, reopened);
});

test('service and auth reconstruction retain browser login without restoring private history', async t => {
  const h = await setup(t);
  const page = await h.page();
  await page.login(); await page.ready();
  await privateConversation(h, page);
  await h.control('restart-service'); // HTTP closes before auth; same auth directory and hash.
  await page.navigate(h.origin);
  await page.ready(); // The existing browser Cookie must still authorize without relogin.
  await assertFreshPrivateView(h, page);
});

// Exercise displayed original text, answer, citation and an unsubmitted draft.
// Only public UI/model seams are used; never inspect auth or Chrome disk formats.
async function privateConversation(h, page) {
  await page.submit('重开验收私人原文');
  await page.wait(`document.querySelector('.msg-record')`);
  const time = await page.evaluate(`document.querySelector('.msg-record .msg-meta').textContent.replace('记录时间：', '')`);
  await h.control('model', { kind: 'query', answers: [
    { choices: [{ message: { role: 'assistant', content: '', tool_calls: [{ id: 'search', type: 'function', function: { name: 'search_memory', arguments: JSON.stringify({ keywords: ['重开验收'], limit: 5 }) } }] }, finish_reason: 'tool_calls' }] },
    { choices: [{ message: { role: 'assistant', content: JSON.stringify({ answer: '重开前的私人回答', citations: [time] }) }, finish_reason: 'stop' }] }
  ] });
  await page.submit('重开验收里记录了什么？');
  await page.wait(`document.querySelector('.citation')`);
  assert.equal(await page.evaluate(`document.querySelector('.msg-answer').textContent`), '重开前的私人回答');
  await page.fill('#input', '重开前未提交的私人草稿');
}

async function assertFreshPrivateView(h, page) {
  assert.equal(await page.evaluate(`document.querySelectorAll('.msg-user, .msg-answer, .citation').length`), 0);
  assert.doesNotMatch(await page.evaluate(`document.querySelector('#messages').textContent`), /重开验收|重开前/);
  assert.equal(await page.evaluate(`document.querySelector('#input').value`), '');
  assert.deepEqual(await page.evaluate(`({local:localStorage.length, session:sessionStorage.length, cookie:document.cookie})`), { local: 0, session: 0, cookie: '' });
  assert.equal((await h.control('stats')).calls, 2, 'no private input was replayed');
  await h.control('model', { kind: 'record' });
  await page.submit('重开后主动记录的新原文');
  await page.wait(`document.querySelector('.msg-record')`);
  assert.equal((await h.control('stats')).calls, 3, 'the retained login authorizes a real memory POST');
}

test('expired login keeps only an in-memory draft and never replays it', async t => {
  const h = await setup(t);
  const page = await h.page(await h.context());
  await page.login(); await page.ready();
  await page.submit('此前的私人展示');
  await page.wait(`document.querySelector('.msg-record')`);
  await h.control('advance', { seconds: 7 * 24 * 60 * 60 });
  await page.submit('  过期后未成功提交\n原文  ');
  await page.wait(`!document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
  assert.match(await page.evaluate(`document.querySelector('#auth-status').textContent`), /过期|失效/);
  assert.equal(await page.evaluate(`document.querySelector('#messages').textContent`), '');
  await page.login(); await page.ready();
  assert.equal(await page.evaluate(`document.querySelector('#input').value`), '  过期后未成功提交\n原文  ');
  assert.equal((await h.control('stats')).calls, 1, 'relogin is not permission to replay');
  await page.click('#send');
  await page.wait(`document.querySelector('.msg-record')`);
  assert.equal((await h.control('stats')).calls, 2);
  await page.navigate(h.origin);
  await page.ready();
  assert.equal(await page.evaluate(`document.querySelectorAll('.msg-user').length`), 0);
  assert.equal(await page.evaluate(`document.querySelector('#input').value`), '');
});

test('model failure preserves draft; logout-all requires password and works with model down', async t => {
  const h = await setup(t);
  const page = await h.page(await h.context());
  const other = await h.page(await h.context());
  await h.control('model', { fail: true });
  await page.login('wrong-password');
  await page.wait(`document.querySelector('#auth-status').textContent.includes('密码') && !document.querySelector('#login').disabled`);
  assert.equal(await page.evaluate(`document.querySelector('#password').value`), '');
  await page.login(); await page.ready();
  await other.login(); await other.ready();
  await page.submit('模型失败的原文');
  await page.wait(`document.querySelector('.msg-error')`);
  assert.equal(await page.evaluate(`document.querySelector('#input').value`), '模型失败的原文');
  assert.match(await page.evaluate(`document.querySelector('.msg-error').textContent`), /模型/);
  await page.click('#show-logout-all');
  await page.fill('#confirm-password', 'wrong-password');
  await page.click('#logout-all');
  await page.wait(`document.querySelector('#auth-status').textContent.includes('密码不正确')`);
  assert.equal(await page.evaluate(`document.querySelector('#workspace').hidden`), false);
  await page.fill('#confirm-password', 'browser-test-only-password');
  await page.click('#logout-all');
  await page.wait(`!document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
  assert.equal(await page.evaluate(`document.querySelector('#input').value`), '');
  await other.submit('被撤销后不能提交');
  await other.wait(`!document.querySelector('#login-form').hidden`);
  assert.equal((await h.control('stats')).calls, 1, 'revoked device never reaches the model');
});

test('a delayed business response cannot restore the private view after logout and relogin', async t => {
  const h = await setup(t);
  const page = await h.page(await h.context());
  await page.login(); await page.ready();
  await page.send('Fetch.enable', { patterns: [{ urlPattern: '*/messages', requestStage: 'Response' }] });
  const paused = h.cdp.event('Fetch.requestPaused', (_, session) => session === page.sessionId);
  await page.submit('绝不能复活的旧原文');
  const response = await paused;
  await page.click('#logout');
  await page.login();
  const finished = h.cdp.event('Network.loadingFinished', event => event.requestId === response.networkId);
  await page.send('Fetch.continueResponse', { requestId: response.requestId });
  await finished;
  await page.send('Fetch.disable');
  await page.evaluate(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`);
  await page.ready();
  assert.equal(await page.evaluate(`document.querySelector('#messages').textContent`), '');
  assert.equal(await page.evaluate(`document.querySelector('#input').value`), '');
});

test('same-Cookie tabs clear private DOM and draft when another tab logs out', async t => {
  const h = await setup(t);
  const context = await h.context();
  const first = await h.page(context);
  await first.login(); await first.ready();
  await first.submit('跨标签页私人原文');
  await first.wait(`document.querySelector('.msg-record')`);
  await first.fill('#input', '跨标签页草稿');
  const second = await h.page(context);
  await second.ready();
  await second.click('#logout');
  await first.wait(`!document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
  assert.equal(await first.evaluate(`document.querySelector('#messages').textContent`), '');
  assert.equal(await first.evaluate(`document.querySelector('#input').value`), '');
  await second.login(); await second.ready();
  await first.ready();
  assert.equal(await first.evaluate(`document.querySelector('#input').value`), '');
  assert.equal((await h.control('stats')).calls, 1);
});

test('actual back/forward cache restoration checks auth and never restores a private snapshot', async t => {
  const h = await setup(t);
  const context = await h.context();
  const first = await h.page(context);
  await first.login(); await first.ready();
  await first.submit('离开前的私人原文');
  await first.wait(`document.querySelector('.msg-record')`);
  await first.fill('#input', '离开前的私人草稿');
  await first.evaluate(`window.addEventListener('pagehide', () => {
    window.hiddenSnapshot = {text: document.querySelector('#messages').textContent, draft: document.querySelector('#input').value};
  }); window.addEventListener('pageshow', event => { window.restoredFromCache = event.persisted; });`);
  const history = await first.send('Page.getNavigationHistory');
  const entry = history.entries[history.currentIndex];
  await first.navigate(h.origin + '/healthz');
  const second = await h.page(context);
  await second.ready();
  await second.click('#logout');
  await second.wait(`!document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
  const restored = h.cdp.event('Page.frameNavigated', event => event.type === 'BackForwardCacheRestore');
  await first.send('Page.navigateToHistoryEntry', { entryId: entry.id });
  await restored;
  await first.wait(`!document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
  assert.equal(await first.evaluate('window.restoredFromCache'), true, 'exercise actual BFCache, not a synthetic pageshow');
  assert.deepEqual(await first.evaluate('window.hiddenSnapshot'), { text: '', draft: '' });
  assert.equal(await first.evaluate(`document.querySelector('#messages').textContent`), '');
  await first.login(); await first.ready();
  assert.equal(await first.evaluate(`document.querySelector('#input').value`), '');
});

test('password rate limit is distinct from expiry and recovers with controlled time', async t => {
  const h = await setup(t);
  const page = await h.page(await h.context());
  for (let attempt = 0; attempt < 10; attempt++) {
    await page.login('incorrect');
    await page.wait(`!document.querySelector('#login').disabled && document.querySelector('#auth-status').textContent.includes('密码不正确')`);
  }
  await page.login();
  await page.wait(`!document.querySelector('#login').disabled && document.querySelector('#auth-status').textContent.includes('频繁')`);
  assert.equal((await h.control('stats')).calls, 0);
  await h.control('advance', { seconds: 300 });
  await page.login(); await page.ready();
});

test('network failure does not claim logout success, and auth outage is not model failure', async t => {
  const h = await setup(t);
  const page = await h.page(await h.context());
  await page.login(); await page.ready();
  await page.fill('#input', '退出时应清除的草稿');
  await page.send('Network.emulateNetworkConditions', { offline: true, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
  await page.click('#logout');
  await page.wait(`!document.querySelector('#retry-session').hidden`);
  assert.match(await page.evaluate(`document.querySelector('#auth-status').textContent`), /退出尚未确认/);
  assert.equal(await page.evaluate(`document.querySelector('#input').value`), '');
  await page.send('Network.emulateNetworkConditions', { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
  await page.click('#retry-session'); await page.ready();
  assert.equal(await page.evaluate(`document.querySelector('#input').value`), '');
  await h.control('close-auth');
  await page.submit('认证故障不应到达模型');
  await page.wait(`!document.querySelector('#retry-session').hidden`);
  assert.match(await page.evaluate(`document.querySelector('#auth-status').textContent`), /认证服务暂时不可用/);
  assert.equal(await page.evaluate(`document.querySelector('#workspace').hidden`), true);
  assert.equal((await h.control('stats')).calls, 0);
});

test('development anonymous mode is explicit and offers no password or logout-all', async t => {
  const h = await setup(t, { developmentAnonymous: true });
  const page = await h.page(await h.context());
  await page.ready();
  assert.match(await page.evaluate(`document.querySelector('#auth-status').textContent`), /开发匿名模式/);
  assert.equal(await page.evaluate(`document.querySelector('#login-form').hidden`), true);
  assert.equal(await page.evaluate(`document.querySelector('#show-logout-all').hidden`), true);
  await page.submit('显式开发模式原文');
  await page.wait(`document.querySelector('.msg-record')`);
  await page.click('#logout'); await page.ready();
  assert.equal(await page.evaluate(`document.querySelector('#messages').textContent`), '');
  assert.match(await page.evaluate(`document.querySelector('#auth-status').textContent`), /开发匿名模式/);
});

test('original text, model answer and real citation render as text without browser storage', async t => {
  const h = await setup(t);
  const page = await h.page(await h.context());
  await page.login(); await page.ready();
  const original = '引用回归 <img src=x onerror="window.pwned=true">\n<script>window.pwned=true</script>';
  await page.submit(original);
  await page.wait(`document.querySelector('.msg-record')`);
  const time = await page.evaluate(`document.querySelector('.msg-record .msg-meta').textContent.replace('记录时间：', '')`);
  const answer = '<svg onload="window.pwned=true">回答</svg>';
  await h.control('model', { kind: 'query', answers: [
    { choices: [{ message: { role: 'assistant', content: '', tool_calls: [{ id: 'search', type: 'function', function: { name: 'search_memory', arguments: JSON.stringify({ keywords: ['引用回归'], limit: 5 }) } }] }, finish_reason: 'tool_calls' }] },
    { choices: [{ message: { role: 'assistant', content: JSON.stringify({ answer, citations: [time] }) }, finish_reason: 'stop' }] }
  ] });
  await page.submit('引用回归里记录了什么？');
  await page.wait(`document.querySelector('.citation')`);
  assert.equal(await page.evaluate(`document.querySelector('.citation-content').textContent`), original);
  assert.equal(await page.evaluate(`document.querySelector('.msg-answer').textContent`), answer);
  assert.equal(await page.evaluate(`document.querySelectorAll('#messages img, #messages script, #messages svg').length`), 0);
  assert.equal(await page.evaluate(`!!window.pwned`), false);
  assert.deepEqual(await page.evaluate(`({local:localStorage.length, session:sessionStorage.length, cookie:document.cookie})`), { local: 0, session: 0, cookie: '' });
  assert.equal(await page.evaluate(`performance.getEntriesByType('resource').every(entry => new URL(entry.name).origin === location.origin)`), true);
});

test('password form native fallback never puts the password in a URL or body', async t => {
  const h = await setup(t);
  const page = await h.page(await h.context());
  await page.wait(`!document.querySelector('#login').disabled`);
  await page.fill('#password', 'native-fallback-secret');
  const sent = h.cdp.event('Network.requestWillBeSent', event => event.type === 'Document' && event.request.url.includes('/auth/login'));
  await page.evaluate(`HTMLFormElement.prototype.submit.call(document.querySelector('#login-form'))`);
  const event = await sent;
  assert.equal(event.request.method, 'POST');
  assert.equal(event.request.url, h.origin + '/auth/login');
  assert.ok(!event.request.postData?.includes('native-fallback-secret'));
});

test('an old authenticated session check cannot undo a newer cross-tab logout', async t => {
  const h = await setup(t);
  const context = await h.context();
  const first = await h.page(context);
  await first.login(); await first.ready();
  const second = await h.page(context); await second.ready();
  await first.send('Page.bringToFront'); await first.ready();
  await first.send('Fetch.enable', { patterns: [{ urlPattern: '*/auth/session', requestStage: 'Response' }] });
  const oldCheck = h.cdp.event('Fetch.requestPaused', (_, session) => session === first.sessionId);
  await first.submit('旧检查不得授权这个原文');
  const oldResponse = await oldCheck;
  const freshCheck = h.cdp.event('Fetch.requestPaused', (event, session) => session === first.sessionId && event.requestId !== oldResponse.requestId);
  await second.send('Page.bringToFront'); await second.ready();
  await second.click('#logout');
  const freshResponse = await freshCheck;
  await first.send('Fetch.continueResponse', { requestId: freshResponse.requestId });
  await first.wait(`!document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
  const finished = h.cdp.event('Network.loadingFinished', event => event.requestId === oldResponse.networkId);
  await first.send('Fetch.continueResponse', { requestId: oldResponse.requestId });
  await finished;
  await first.send('Fetch.disable');
  await first.evaluate(`new Promise(resolve => {
    const channel = new MessageChannel();
    channel.port1.onmessage = () => { channel.port1.close(); channel.port2.close(); resolve(); };
    channel.port2.postMessage(null);
  })`);
  assert.equal(await first.evaluate(`document.querySelector('#workspace').hidden`), true);
  assert.equal(await first.evaluate(`document.querySelector('#input').value`), '');
  assert.equal((await h.control('stats')).calls, 0);
});

test('another device can revoke an admitted request before its response is displayed', async t => {
  const h = await setup(t);
  const page = await h.page(await h.context());
  const other = await h.page(await h.context());
  await page.login(); await page.ready();
  await other.login(); await other.ready();
  await page.send('Fetch.enable', { patterns: [{ urlPattern: '*/messages', requestStage: 'Response' }] });
  const paused = h.cdp.event('Fetch.requestPaused', (_, session) => session === page.sessionId);
  await page.submit('远端撤销后的旧结果不得展示');
  const response = await paused;
  await other.click('#show-logout-all');
  await other.fill('#confirm-password', 'browser-test-only-password');
  await other.click('#logout-all');
  await other.wait(`!document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
  await page.send('Fetch.continueResponse', { requestId: response.requestId });
  await page.send('Fetch.disable');
  await page.wait(`!document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
  assert.equal(await page.evaluate(`document.querySelector('#messages').textContent`), '');
  assert.equal((await h.control('stats')).calls, 1, 'already admitted work is not promised to be rolled back');
});

test('real CSRF rejection is distinct from model failure and never retries the original text', async t => {
  const h = await setup(t);
  const page = await h.page(await h.context());
  await page.login(); await page.ready();
  await page.send('Fetch.enable', { patterns: [{ urlPattern: '*/messages', requestStage: 'Request' }] });
  const paused = h.cdp.event('Fetch.requestPaused', (_, session) => session === page.sessionId);
  await page.submit('CSRF 拒绝后应保留的原文');
  const request = await paused;
  const headers = Object.entries(request.request.headers).map(([name, value]) => ({ name, value: name.toLowerCase() === 'x-csrf-token' ? 'invalid-test-token' : value }));
  await page.send('Fetch.continueRequest', { requestId: request.requestId, headers });
  await page.wait(`document.querySelector('#auth-status').textContent.includes('安全校验') && !document.querySelector('#send').disabled`);
  await page.send('Fetch.disable');
  assert.equal(await page.evaluate(`document.querySelector('#input').value`), 'CSRF 拒绝后应保留的原文');
  assert.equal(await page.evaluate(`document.querySelectorAll('.msg-error').length`), 0);
  assert.equal((await h.control('stats')).calls, 0);
  await page.click('#send');
  await page.wait(`document.querySelector('.msg-record')`);
  assert.equal((await h.control('stats')).calls, 1);
});

test('current logout clears private view and draft without logging out another device', async t => {
  const h = await setup(t);
  const first = await h.page(await h.context());
  const second = await h.page(await h.context());
  await first.login(); await first.ready();
  await second.login(); await second.ready();
  await first.submit('要清除的私人原文');
  await first.wait(`document.querySelector('.msg-record')`);
  await first.fill('#input', '要清除的草稿');
  await first.click('#logout');
  await first.wait(`!document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
  assert.equal(await first.evaluate(`document.querySelector('#messages').textContent`), '');
  assert.equal(await first.evaluate(`document.querySelector('#input').value`), '');
  await second.submit('另一设备仍可记录');
  await second.wait(`document.querySelector('.msg-record')`);
  assert.equal((await h.control('stats')).calls, 2);
  await h.control('advance', { seconds: 7 * 24 * 60 * 60 });
  await second.click('#logout');
  await second.wait(`!document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
  assert.match(await second.evaluate(`document.querySelector('#auth-status').textContent`), /过期|失效/);
});
