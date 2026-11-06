// No npm dependencies: Node 24 supplies fetch, WebSocket and node:test.
import { spawn } from 'node:child_process';
import { once, EventEmitter } from 'node:events';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createInterface } from 'node:readline';

export const password = 'browser-test-only-password';
const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const timeout = 10000;

function deadline(promise, label, ms = timeout) {
  let timer;
  return Promise.race([promise, new Promise((_, reject) => {
    timer = setTimeout(() => reject(new Error(`Timed out: ${label}`)), ms);
  })]).finally(() => clearTimeout(timer));
}

function child(command, args, options = {}) {
  const process = spawn(command, args, { cwd: root, stdio: ['pipe', 'pipe', 'pipe'], ...options });
  process.finished = once(process, 'exit');
  // Retain diagnostics, but never print browser cookies or fixture input.
  process.errors = '';
  process.stderr.on('data', chunk => { process.errors += chunk; });
  return process;
}

async function stop(process) {
  if (!process || process.exitCode !== null || process.signalCode !== null) return;
  process.kill('SIGTERM');
  try { await deadline(process.finished, 'child exit', 3000); }
  catch { process.kill('SIGKILL'); await process.finished; }
}

export class CDP extends EventEmitter {
  constructor(socket) {
    super();
    this.socket = socket;
    this.next = 0;
    this.pending = new Map();
    socket.addEventListener('message', event => {
      const message = JSON.parse(event.data);
      if (message.id) {
        const pending = this.pending.get(message.id);
        if (!pending) return;
        this.pending.delete(message.id);
        if (message.error) pending.reject(new Error(message.error.message));
        else pending.resolve(message.result);
      } else this.emit(message.method, message.params, message.sessionId);
    });
    socket.addEventListener('close', () => {
      for (const pending of this.pending.values()) pending.reject(new Error('CDP closed'));
      this.pending.clear();
    });
  }
  static async connect(url) {
    const socket = new WebSocket(url);
    await deadline(new Promise((resolve, reject) => {
      socket.addEventListener('open', resolve, { once: true });
      socket.addEventListener('error', reject, { once: true });
    }), 'CDP connection');
    return new CDP(socket);
  }
  async send(method, params = {}, sessionId) {
    const id = ++this.next;
    const promise = new Promise((resolve, reject) => this.pending.set(id, { resolve, reject }));
    this.socket.send(JSON.stringify({ id, method, params, sessionId }));
    return deadline(promise, method).finally(() => this.pending.delete(id));
  }
  event(method, predicate = () => true) {
    let listener;
    const result = deadline(new Promise(resolve => {
      listener = (params, session) => { if (predicate(params, session)) resolve(params); };
      this.on(method, listener);
    }), method).finally(() => this.off(method, listener));
    // A failed earlier assertion may abandon an already-armed event waiter.
    // Keep its rejection handled; an actual await still receives the failure.
    result.catch(() => {});
    return result;
  }
}

export class Page {
  constructor(cdp, sessionId, targetId) { Object.assign(this, { cdp, sessionId, targetId }); }
  send(method, params) { return this.cdp.send(method, params, this.sessionId); }
  async evaluate(expression) {
    const result = await this.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true });
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
    return result.result.value;
  }
  async wait(expression) {
    // DOM observations, not fixed sleeps. Recheck input/visibility state on events too.
    return this.evaluate(`new Promise((resolve, reject) => {
      const check = () => { if (${expression}) { cleanup(); resolve(true); } };
      const observer = new MutationObserver(check);
      const timer = setTimeout(() => { cleanup(); reject(new Error('DOM condition: ' + ${JSON.stringify(expression)})); }, 6000);
      const cleanup = () => { clearTimeout(timer); observer.disconnect(); document.removeEventListener('input', check); };
      observer.observe(document, {subtree:true, childList:true, attributes:true, characterData:true});
      document.addEventListener('input', check); check();
    })`);
  }
  async navigate(url) {
    const loaded = this.cdp.event('Page.loadEventFired', (_, session) => session === this.sessionId);
    await this.send('Page.navigate', { url });
    await loaded;
  }
  async fill(selector, value) {
    await this.evaluate(`(() => { const node = document.querySelector(${JSON.stringify(selector)}); node.value = ${JSON.stringify(value)}; node.dispatchEvent(new Event('input', {bubbles:true})); })()`);
  }
  async click(selector) {
    await this.evaluate(`document.querySelector(${JSON.stringify(selector)}).click()`);
  }
  async login(value = password) {
    await this.wait(`document.querySelector('#login-form') && !document.querySelector('#login-form').hidden && !document.querySelector('#login').disabled`);
    await this.fill('#password', value);
    await this.click('#login');
  }
  async ready() { await this.wait(`!document.querySelector('#workspace').hidden && !document.querySelector('#send').disabled`); }
  async submit(content) {
    await this.ready();
    await this.fill('#input', content);
    await this.click('#send');
  }
}

export async function setup(t, { developmentAnonymous = false } = {}) {
  const directory = await mkdtemp(join(tmpdir(), 'shiyi-browser-'));
  let build, fixture, chrome, cdp;
  t.after(async () => {
    if (cdp) {
      try { await cdp.send('Browser.close'); await deadline(chrome.finished, 'Chrome shutdown', 3000); }
      catch { /* force termination below if the browser crashed */ }
      cdp.socket.close();
    }
    await stop(chrome);
    if (fixture) {
      fixture.stdin.end();
      try { await deadline(fixture.finished, 'fixture shutdown', 3000); }
      catch { await stop(fixture); }
    }
    await stop(build);
    // Only remove the exact mkdtemp directory owned by this invocation.
    if (resolve(directory) !== directory || dirname(directory) !== resolve(tmpdir()) || !directory.startsWith(join(tmpdir(), 'shiyi-browser-'))) throw new Error('Unsafe cleanup path');
    await rm(directory, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
  });
  build = child('go', ['build', '-o', join(directory, 'fixture'), './tests/browser/fixture']);
  const [code] = await deadline(build.finished, 'fixture build', 60000);
  if (code !== 0) throw new Error(`Fixture build failed: ${build.errors}`);
  fixture = child(join(directory, 'fixture'), [], { env: { ...process.env, SHIYI_BROWSER_TEST_DIR: directory, SHIYI_BROWSER_DEV_ANONYMOUS: developmentAnonymous ? '1' : '0' } });
  const lines = createInterface({ input: fixture.stdout });
  const ready = new Promise(resolve => lines.once('line', line => resolve(JSON.parse(line))));
  const { origin, spki } = await deadline(Promise.race([ready, fixture.finished.then(() => { throw new Error(fixture.errors); })]), 'fixture ready');
  let next = 0;
  const pending = new Map();
  lines.on('line', line => {
    const response = JSON.parse(line);
    pending.get(response.id)?.(response);
    pending.delete(response.id);
  });
  const control = (op, extra = {}) => {
    const id = ++next;
    const reply = new Promise(resolve => pending.set(id, resolve));
    fixture.stdin.write(JSON.stringify({ id, op, ...extra }) + '\n');
    return deadline(reply, `fixture ${op}`);
  };
  const args = ['--headless=new', '--remote-debugging-address=127.0.0.1', '--remote-debugging-port=0',
    `--user-data-dir=${join(directory, 'chrome')}`, `--ignore-certificate-errors-spki-list=${spki}`,
    '--no-first-run', '--no-default-browser-check', '--disable-background-networking', '--disable-component-update',
    // Persistent test profiles must not contact the user's desktop keyring.
    '--password-store=basic', '--disable-sync', '--disable-default-apps', '--disable-dev-shm-usage', 'about:blank'];
  // Explicit opt-in for sandbox-restricted CI; never attach to a user's browser.
  if (process.env.CHROME_NO_SANDBOX === '1') args.unshift('--no-sandbox');
  async function startChrome() {
    chrome = child(process.env.CHROME_BIN || '/bin/google-chrome', args, {
      env: { ...process.env, HOME: directory, XDG_CONFIG_HOME: join(directory, 'config'), XDG_CACHE_HOME: join(directory, 'cache') }
    });
    const debuggerURL = new Promise(resolve => {
      chrome.stderr.on('data', () => {
        const match = chrome.errors.match(/DevTools listening on (ws:\/\/[^\s]+)/);
        if (match) resolve(match[1]);
      });
    });
    const url = await deadline(Promise.race([debuggerURL, chrome.finished.then(() => { throw new Error(`Chrome failed: ${chrome.errors}`); })]), 'Chrome ready');
    cdp = await CDP.connect(url);
  }
  async function restartBrowser() {
    const previousPID = chrome.pid;
    await cdp.send('Browser.close');
    // A forced kill is not a successful normal close/reopen acceptance.
    const [exitCode] = await deadline(chrome.finished, 'Chrome normal shutdown');
    cdp.socket.close();
    cdp = undefined;
    await startChrome(); // Same mkdtemp profile; no Cookie export/import.
    return { previousPID, pid: chrome.pid, exitCode };
  }
  await startChrome();
  async function context() { return (await cdp.send('Target.createBrowserContext')).browserContextId; }
  async function page(browserContextId) {
    const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank', browserContextId });
    const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true });
    const p = new Page(cdp, sessionId, targetId);
    await p.send('Runtime.enable');
    await p.send('Page.enable');
    await p.send('Network.enable');
    await p.navigate(origin);
    return p;
  }
  return { origin, control, get cdp() { return cdp; }, context, page, restartBrowser };
}
