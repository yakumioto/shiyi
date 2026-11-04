// Shiyi 极简 Web 界面。
//
// 规则（安全）：
//   * 所有来自服务端或用户的文本一律用 textContent 写入；
//     页面中不存在把字符串当作标记解析的写法，模型输出里的 HTML 或脚本不会被执行。
//   * 页面不在浏览器里保存任何东西：没有浏览器端存储，没有 cookie，没有历史；
//     刷新后一切自然消失。
//   * 每次发送都是独立的一次 POST /messages，请求体只有当次的 content，
//     不携带任何历史上下文。
//   * 除 POST /messages 外页面不发起任何其它请求（无 CDN、无遥测）。

(function () {
  'use strict';

  // API 入口由当前页面地址推导，因此页面挂在 "/" 或任何前缀下都能打到同级的
  // /messages；不使用写死的绝对路径。
  var endpoint = new URL('messages', window.location.href).toString();

  var formEl = document.getElementById('form');
  var inputEl = document.getElementById('input');
  var sendEl = document.getElementById('send');
  var listEl = document.getElementById('messages');

  // ---------- DOM 辅助 ----------

  // el 只用 createElement + textContent 构造节点，参数永远不会被当作标记解析。
  function el(tag, className, text) {
    var node = document.createElement(tag);
    if (className) {
      node.className = className;
    }
    if (text !== undefined && text !== null) {
      node.textContent = String(text);
    }
    return node;
  }

  function append(node) {
    var hint = listEl.querySelector('.msg-hint');
    if (hint) {
      hint.remove();
    }
    listEl.appendChild(node);
    listEl.scrollTop = listEl.scrollHeight;
  }

  // ---------- 渲染 ----------

  function appendUser(content) {
    var item = el('li', 'msg msg-user');
    item.appendChild(el('p', 'msg-role', '你'));
    item.appendChild(el('p', 'msg-text', content));
    append(item);
  }

  function appendRecord(body) {
    var item = el('li', 'msg msg-record');
    item.appendChild(el('p', 'msg-role', '已记录'));
    var text = el('p', 'msg-text', '这句话已经被保存为记忆。');
    item.appendChild(text);
    item.appendChild(el('p', 'msg-meta', '记录时间：' + (body.time || '')));
    append(item);
  }

  function appendUnrecognized() {
    var item = el('li', 'msg msg-unrecognized');
    item.appendChild(el('p', 'msg-role', '无法识别'));
    item.appendChild(el('p', 'msg-text', '无法识别，这条没有被保存。'));
    append(item);
  }

  function appendQuery(body) {
    var item = el('li', 'msg msg-query');
    item.appendChild(el('p', 'msg-role', '回答'));
    item.appendChild(el('p', 'msg-answer', body.answer === undefined || body.answer === null ? '' : body.answer));

    var statusText = body.status || '';
    item.appendChild(el('p', 'msg-meta', '状态：' + statusText + '（' + describeStatus(statusText) + '）'));

    var citations = Array.isArray(body.citations) ? body.citations : [];
    if (citations.length === 0) {
      item.appendChild(el('p', 'msg-meta', '引用：无'));
      append(item);
      return;
    }
    item.appendChild(el('p', 'msg-meta', '引用：' + citations.length + ' 条'));
    var box = el('div', 'citations');
    for (var i = 0; i < citations.length; i++) {
      box.appendChild(citationBlock(citations[i]));
    }
    item.appendChild(box);
    append(item);
  }

  // citationBlock 用 createElement + textContent 构造引用块，原文只当纯文本显示。
  function citationBlock(citation) {
    var block = el('div', 'citation');
    var source = citation || {};
    block.appendChild(el('div', 'citation-time', '记录时间：' + (source.time || '')));
    block.appendChild(el('div', 'citation-content', source.content === undefined || source.content === null ? '' : source.content));
    return block;
  }

  function describeStatus(status) {
    if (status === 'ok') {
      return '正常';
    }
    if (status === 'budget_exhausted') {
      return '检索预算已用尽';
    }
    if (status === 'no_match') {
      return '没有找到相关记忆';
    }
    return '未知状态';
  }

  function appendError(code, message, status) {
    var item = el('li', 'msg msg-error');
    item.appendChild(el('p', 'msg-role', '出错了'));
    item.appendChild(el('p', 'msg-meta', '错误代码：' + (code || 'error')));
    var line = '错误信息：' + (message || '未知错误');
    if (status) {
      line += '（HTTP ' + status + '）';
    }
    item.appendChild(el('p', 'msg-text', line));
    append(item);
  }

  // ---------- 发送 ----------

  async function send(content) {
    var res;
    try {
      res = await fetch(endpoint, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({content: content})
      });
    } catch (err) {
      appendError('network_error', '无法连接服务器，请确认服务正在运行。', 0);
      return;
    }

    var body = null;
    try {
      body = await res.json();
    } catch (err) {
      body = null;
    }

    if (body && body.error) {
      appendError(body.error.code || 'error', body.error.message || '未知错误', res.status);
      return;
    }
    if (!res.ok) {
      appendError('http_error', '服务器返回了 ' + res.status + '。', res.status);
      return;
    }
    if (!body || typeof body.kind !== 'string') {
      appendError('internal_error', '服务器返回了无法识别的响应。', res.status);
      return;
    }
    if (body.kind === 'record') {
      appendRecord(body);
      return;
    }
    if (body.kind === 'query') {
      appendQuery(body);
      return;
    }
    if (body.kind === 'unrecognized') {
      appendUnrecognized();
      return;
    }
    appendError('internal_error', '服务器返回了未知的响应类型。', res.status);
  }

  // ---------- 交互 ----------

  var busy = false;

  function submit() {
    if (busy) {
      return;
    }
    var content = inputEl.value;
    if (!content || !content.trim()) {
      inputEl.focus();
      return;
    }

    busy = true;
    sendEl.disabled = true;
    inputEl.value = '';
    appendUser(content);

    send(content).then(function () {
      busy = false;
      sendEl.disabled = false;
      inputEl.focus();
    }, function () {
      busy = false;
      sendEl.disabled = false;
      inputEl.focus();
    });
  }

  formEl.addEventListener('submit', function (event) {
    event.preventDefault();
    submit();
  });

  // Enter 发送，Shift+Enter 换行。
  inputEl.addEventListener('keydown', function (event) {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      submit();
    }
  });

  inputEl.focus();
})();
