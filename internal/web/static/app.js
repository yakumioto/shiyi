// Shiyi: 原文、回答、引用只用 textContent；草稿与 CSRF token 只留本页内存。
// HttpOnly Cookie 由浏览器管理，脚本不读凭据；不保存历史，不加载第三方资源。
(function () {
  'use strict';

  var endpoint = new URL('/messages', window.location.href).toString();
  var formEl = document.getElementById('form');
  var inputEl = document.getElementById('input');
  var sendEl = document.getElementById('send');
  var listEl = document.getElementById('messages');
  var workspaceEl = document.getElementById('workspace');
  var loginFormEl = document.getElementById('login-form');
  var loginEl = document.getElementById('login');
  var passwordEl = document.getElementById('password');
  var statusEl = document.getElementById('auth-status');
  var retryEl = document.getElementById('retry-session');
  var logoutEl = document.getElementById('logout');
  var showAllEl = document.getElementById('show-logout-all');
  var allFormEl = document.getElementById('logout-all-form');
  var allEl = document.getElementById('logout-all');
  var confirmEl = document.getElementById('confirm-password');

  var csrf = '';
  var identity = '';
  var mode = 'password';
  var authenticated = false;
  var ready = false;
  var busy = false;
  var authBusy = false;
  var draft = '';
  // Every async continuation must belong to the current private-view generation.
  // A logout invalidates even already-admitted requests; their results cannot repaint.
  var generation = 0;
  var channel = null;

  // Notifications contain no credentials, draft or private text, and persist nothing.
  function connectTabs() {
    if (channel || !window.BroadcastChannel) return;
    channel = new BroadcastChannel('shiyi-auth');
    channel.onmessage = function (event) {
      if (event.data !== 'logout' && event.data !== 'login') return;
      generation++;
      authBusy = false;
      clearPrivate(event.data === 'login' && !identity);
      identity = '';
      csrf = '';
      ready = false;
      checkSession(event.data === 'logout' ? '已在另一标签页退出。私人展示和草稿已清除。' : undefined);
    };
  }
  function announce(event) { if (channel) channel.postMessage(event); }

  function controls() {
    workspaceEl.hidden = !authenticated;
    loginFormEl.hidden = authenticated || mode === 'development_anonymous';
    loginEl.disabled = authBusy || !ready;
    passwordEl.disabled = authBusy || !ready;
    retryEl.hidden = ready || authBusy;
    sendEl.disabled = !authenticated || busy || authBusy;
    inputEl.disabled = busy || authBusy;
    logoutEl.disabled = authBusy;
    showAllEl.disabled = authBusy;
    showAllEl.hidden = mode === 'development_anonymous';
    allEl.disabled = authBusy;
    confirmEl.disabled = authBusy;
  }

  function clearPrivate(keepDraft) {
    if (authenticated) draft = inputEl.value;
    if (!keepDraft) draft = '';
    inputEl.value = '';
    listEl.textContent = '';
    passwordEl.value = '';
    confirmEl.value = '';
    allFormEl.hidden = true;
    authenticated = false;
    busy = false;
    controls();
  }

  function unavailable(message) {
    clearPrivate(true);
    ready = false;
    csrf = '';
    statusEl.textContent = message || '认证服务暂时不可用，请重试检查登录状态。草稿仅保留在本页。';
    controls();
  }

  async function request(path, payload, token) {
    var options = {credentials: 'same-origin', cache: 'no-store'};
    if (token !== undefined) {
      options.method = 'POST';
      options.headers = {'Content-Type': 'application/json', 'X-CSRF-Token': token};
      if (payload !== undefined) options.body = JSON.stringify(payload);
    }
    var res = await fetch(path, options);
    var body = null;
    if (res.status !== 204) {
      try { body = await res.json(); } catch (_) { /* malformed response is not success */ }
    }
    return {res: res, body: body};
  }

  function authError(result) {
    var code = result.body && result.body.error && result.body.error.code;
    if (code === 'unauthorized') return '密码不正确。';
    if (code === 'rate_limited') {
      var seconds = result.res.headers.get('Retry-After');
      return '密码尝试过于频繁，请' + (/^\d+$/.test(seconds) ? '在 ' + seconds + ' 秒后' : '稍后') + '重试。';
    }
    if (code === 'forbidden') return '安全校验已失效，请检查登录状态后重新操作。';
    if (code === 'auth_unavailable') return '认证服务暂时不可用，请稍后重试。';
    return '认证操作暂时失败，请稍后重试。';
  }

  function applySession(body, notice) {
    if (!body || typeof body.authenticated !== 'boolean' || typeof body.csrf_token !== 'string' ||
        !body.csrf_token || ['password', 'development_anonymous'].indexOf(body.mode) === -1) {
      throw new Error('invalid session response');
    }
    var expired = !!identity && !body.authenticated;
    // Password-session tokens identify a login. Development-anonymous challenges
    // can rotate without changing identity or discarding the current draft.
    if (body.authenticated && identity && (mode !== body.mode ||
        (body.mode === 'password' && identity !== body.csrf_token))) {
      generation++;
      authBusy = false;
      clearPrivate(false);
      notice = '登录状态已变更，旧私人展示和草稿已清除。';
    }
    if (!body.authenticated) clearPrivate(true);
    csrf = body.csrf_token;
    identity = body.authenticated ? csrf : '';
    mode = body.mode;
    authenticated = body.authenticated;
    ready = true;
    if (authenticated) inputEl.value = draft;
    statusEl.textContent = (mode === 'development_anonymous' ? '开发匿名模式：未启用密码保护。' : '') +
      (notice || (authenticated ? '已登录。每次输入独立处理，不携带对话上下文。' :
        (expired ? '登录会话已过期或失效，请重新登录。草稿仅留本页，登录后需主动发送。' : '请登录。')));
    controls();
  }

  async function probe(ticket, notice) {
    try {
      var result = await request('/auth/session');
      if (ticket !== generation) return false;
      if (!result.res.ok) { unavailable(authError(result)); return false; }
      applySession(result.body, notice);
      return authenticated && ticket === generation;
    } catch (_) {
      if (ticket === generation) unavailable('无法连接认证服务，请重试检查登录状态。草稿仅保留在本页。');
      return false;
    }
  }

  async function checkSession(notice) {
    var ticket = ++generation;
    busy = false;
    authBusy = true;
    controls();
    await probe(ticket, notice);
    if (ticket === generation) { authBusy = false; controls(); }
  }

  async function passwordAction(all) {
    if (authBusy || !ready || (all && !authenticated)) return;
    var password = all ? confirmEl.value : passwordEl.value;
    passwordEl.value = '';
    confirmEl.value = '';
    if (!password) return;
    var ticket = ++generation;
    busy = false;
    authBusy = true;
    controls();
    try {
      // Another tab may have refreshed a bootstrap challenge or rotated the Cookie.
      // Refresh CSRF for this explicit action; never replay the password POST itself.
      var stillAuthenticated = await probe(ticket);
      if (ticket !== generation || !ready || (all && !stillAuthenticated)) return;
      var result = await request(all ? '/auth/logout-all' : '/auth/login', {password: password}, csrf);
      if (ticket !== generation) return;
      if (all && result.res.status === 204) {
        clearPrivate(false);
        identity = '';
        csrf = '';
        await probe(ticket, '已退出所有设备。');
        if (ticket === generation) announce('logout');
      } else if (!all && result.res.ok) {
        applySession(result.body, draft ? '已登录。草稿已保留，请确认后主动发送。' : '已登录。');
        announce('login');
      } else {
        // A 401 from logout-all could mean either a wrong password or expiry.
        // Check the real session instead of labelling an expired session a bad password.
        var message = authError(result);
        var valid = await probe(ticket);
        if (ticket === generation && ready && (valid || !all)) statusEl.textContent = message;
      }
    } catch (_) {
      if (ticket === generation) unavailable('无法连接认证服务；操作结果未确认，请检查登录状态后重试。');
    } finally {
      if (ticket === generation) { authBusy = false; controls(); }
    }
  }

  async function logout() {
    if (authBusy || !authenticated) return;
    var token = csrf;
    var ticket = ++generation;
    clearPrivate(false);
    authBusy = true;
    ready = false;
    statusEl.textContent = '正在退出当前设备……';
    controls();
    try {
      var result = await request('/auth/logout', undefined, token);
      if (ticket !== generation) return;
      identity = '';
      csrf = '';
      var ended = result.res.status === 204 || result.res.status === 401;
      var notice = result.res.status === 204 ? '已退出当前设备。' :
        (result.res.status === 401 ? '登录会话已过期或失效，私人展示和草稿已清除。' : '退出未确认：' + authError(result));
      await probe(ticket, notice);
      if (ticket === generation && ended) announce('logout');
    } catch (_) {
      if (ticket === generation) unavailable('无法连接认证服务，退出尚未确认。私人展示已清除，请重试检查并退出。');
    } finally {
      if (ticket === generation) { authBusy = false; controls(); }
    }
  }

  // ---------- 安全文本渲染 ----------
  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined && text !== null) node.textContent = String(text);
    return node;
  }
  function append(node) {
    var hint = listEl.querySelector('.msg-hint');
    if (hint) hint.remove();
    listEl.appendChild(node);
    listEl.scrollTop = listEl.scrollHeight;
  }
  function appendUser(content) {
    var item = el('li', 'msg msg-user');
    item.appendChild(el('p', 'msg-role', '你'));
    item.appendChild(el('p', 'msg-text', content));
    append(item);
  }
  function appendRecord(body) {
    var item = el('li', 'msg msg-record');
    item.appendChild(el('p', 'msg-role', '已记录'));
    item.appendChild(el('p', 'msg-text', '这句话已经被保存为记忆。'));
    item.appendChild(el('p', 'msg-meta', '记录时间：' + (body.time || '')));
    append(item);
  }
  function appendUnrecognized() {
    var item = el('li', 'msg msg-unrecognized');
    item.appendChild(el('p', 'msg-role', '无法识别'));
    item.appendChild(el('p', 'msg-text', '无法识别，这条没有被保存。'));
    append(item);
  }
  function describeStatus(status) {
    if (status === 'ok') return '正常';
    if (status === 'budget_exhausted') return '检索预算已用尽';
    if (status === 'no_match') return '没有找到相关记忆';
    return '未知状态';
  }
  function appendQuery(body) {
    var item = el('li', 'msg msg-query');
    item.appendChild(el('p', 'msg-role', '回答'));
    item.appendChild(el('p', 'msg-answer', body.answer || ''));
    var status = body.status || '';
    item.appendChild(el('p', 'msg-meta', '状态：' + status + '（' + describeStatus(status) + '）'));
    var citations = Array.isArray(body.citations) ? body.citations : [];
    item.appendChild(el('p', 'msg-meta', citations.length ? '引用：' + citations.length + ' 条' : '引用：无'));
    var box = el('div', 'citations');
    citations.forEach(function (citation) {
      var source = citation || {};
      var block = el('div', 'citation');
      block.appendChild(el('div', 'citation-time', '记录时间：' + (source.time || '')));
      block.appendChild(el('div', 'citation-content', source.content || ''));
      box.appendChild(block);
    });
    item.appendChild(box);
    append(item);
  }
  function appendError(code, message, status) {
    var item = el('li', 'msg msg-error');
    item.appendChild(el('p', 'msg-role', '提交未成功，原文已保留；请确认后主动重试'));
    item.appendChild(el('p', 'msg-meta', '错误代码：' + (code || 'error')));
    item.appendChild(el('p', 'msg-text', (message || '未知错误') + (status ? '（HTTP ' + status + '）' : '')));
    append(item);
  }

  async function submit() {
    if (busy || authBusy || !authenticated) return;
    var content = inputEl.value;
    if (!content || !content.trim()) { inputEl.focus(); return; }
    draft = content;
    busy = true;
    var ticket = generation;
    controls();
    try {
      // A foreground interaction verifies the Cookie even without cross-tab notifications.
      if (!await probe(ticket) || ticket !== generation) return;
      var result = await request(endpoint, {content: content}, csrf);
      if (ticket !== generation) return;
      if (result.res.status === 401 || result.res.status === 403) {
        clearPrivate(true);
        await probe(ticket, result.res.status === 401 ?
          '登录会话已过期或失效，请重新登录。草稿仅留本页，登录后需主动发送。' :
          '安全校验已失效，请检查登录状态后主动重试；未自动重发。');
        return;
      }
      if (result.body && result.body.error && result.body.error.code === 'auth_unavailable') {
        unavailable(authError(result));
        return;
      }
      // A request admitted before revocation can still finish; check again before rendering it.
      if (!await probe(ticket) || ticket !== generation) return;
      var body = result.body;
      if (!result.res.ok || !body || ['record', 'query', 'unrecognized'].indexOf(body.kind) === -1) {
        var error = body && body.error || {};
        appendError(error.code || 'internal_error', error.message || '服务器响应异常，请稍后重试。', result.res.status);
        return;
      }
      draft = '';
      inputEl.value = '';
      appendUser(content);
      if (body.kind === 'record') appendRecord(body);
      if (body.kind === 'query') appendQuery(body);
      if (body.kind === 'unrecognized') appendUnrecognized();
    } catch (_) {
      if (ticket === generation) appendError('network_error', '无法连接服务器。提交结果未确认，重试可能重复记录。', 0);
    } finally {
      if (ticket === generation) { busy = false; controls(); }
    }
  }

  loginFormEl.addEventListener('submit', function (event) { event.preventDefault(); passwordAction(false); });
  allFormEl.addEventListener('submit', function (event) { event.preventDefault(); passwordAction(true); });
  logoutEl.addEventListener('click', logout);
  showAllEl.addEventListener('click', function () { allFormEl.hidden = false; confirmEl.focus(); });
  document.getElementById('cancel-logout-all').addEventListener('click', function () {
    allFormEl.hidden = true;
    confirmEl.value = '';
  });
  retryEl.addEventListener('click', function () { checkSession(); });
  formEl.addEventListener('submit', function (event) { event.preventDefault(); submit(); });
  inputEl.addEventListener('input', function () { draft = inputEl.value; });
  inputEl.addEventListener('keydown', function (event) {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); submit(); }
  });

  var suspended = false;
  function suspend(leaving) {
    generation++;
    authBusy = false;
    // Leaving the page clears the draft too, so a BFCache snapshot cannot keep it
    // across a logout that happens while this document is frozen.
    clearPrivate(!leaving);
    ready = false;
    suspended = true;
    statusEl.textContent = '恢复页面后将重新检查登录状态。';
    controls();
    if (leaving && channel) { channel.close(); channel = null; }
  }
  function resume() {
    if (!suspended) return;
    suspended = false;
    connectTabs();
    checkSession();
  }
  window.addEventListener('pagehide', function () { suspend(true); });
  window.addEventListener('pageshow', resume);
  document.addEventListener('visibilitychange', function () {
    if (document.visibilityState === 'hidden') suspend(false);
    else resume();
  });
  window.addEventListener('focus', function () {
    if (suspended) resume();
    else if (!authBusy && !busy) {
      clearPrivate(true);
      checkSession();
    }
  });

  connectTabs();
  checkSession();
})();
