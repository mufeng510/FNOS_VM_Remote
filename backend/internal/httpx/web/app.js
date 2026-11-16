/* FNOS VM Remote 前端逻辑：原生 fetch，Token 存 sessionStorage，绝不写入日志。 */
'use strict';

const $ = (sel) => document.querySelector(sel);

const state = {
  token: sessionStorage.getItem('vmremote_token') || '',
  busy: false,
  refreshTimer: null,
};

/* ---------- 工具函数 ---------- */

function showToast(msg) {
  const toast = $('#toast');
  toast.textContent = msg;
  toast.classList.remove('hidden');
  setTimeout(() => toast.classList.add('hidden'), 3200);
}

function setBusy(isBusy) {
  state.busy = isBusy;
  for (const id of ['#btn-start', '#btn-shutdown', '#btn-refresh']) {
    $(id).disabled = isBusy;
  }
}

function setActionMessage(text, kind) {
  const el = $('#action-message');
  if (!text) { el.classList.add('hidden'); return; }
  el.textContent = text;
  el.className = 'action-message' + (kind ? ' ' + kind : '');
}

// 统一请求：自动带 Bearer，401 时踢回登录视图
async function api(path, options = {}) {
  const resp = await fetch(path, {
    ...options,
    headers: {
      'Authorization': 'Bearer ' + state.token,
      ...(options.headers || {}),
    },
  });
  let body = null;
  try { body = await resp.json(); } catch { /* 非 JSON */ }
  if (resp.status === 401) {
    sessionStorage.removeItem('vmremote_token');
    state.token = '';
    showLogin('Token 无效或已过期，请重新输入');
    throw new Error('unauthorized');
  }
  return { status: resp.status, body };
}

function fmtTime(iso) {
  if (!iso) return '—';
  try {
    const d = new Date(iso);
    return d.toLocaleString('zh-CN', { hour12: false });
  } catch { return iso; }
}

const STATE_LABEL = {
  'running': '运行中',
  'shut off': '已关闭',
  'starting': '启动中',
  'shutting_down': '关机中',
  'paused': '已暂停',
  'unknown': '未知',
};

function stateClass(s) {
  if (s === 'running') return 'state-running';
  if (s === 'shut off') return 'state-off';
  if (s === 'starting' || s === 'shutting_down') return 'state-busy';
  return 'state-unknown';
}

function renderStatus(payload) {
  const badge = $('#state-badge');
  const s = payload.state || 'unknown';
  badge.textContent = STATE_LABEL[s] || s;
  badge.className = 'state-badge ' + stateClass(s);
  $('#state-updated').textContent = '最后更新：' + fmtTime(payload.updated_at);
  $('#vm-name').textContent = payload.name || '—';
  $('#vm-uuid').textContent = payload.uuid || '—';
  $('#vm-raw').textContent = payload.raw_state || '—';
}

/* ---------- 数据加载 ---------- */

async function refreshStatus() {
  try {
    const { body } = await api('/api/v1/vm/status');
    if (body && body.success) renderStatus(body.data);
    else setActionMessage((body && body.error) || '状态查询失败', 'err');
  } catch (err) {
    if (err.message !== 'unauthorized') {
      setActionMessage('状态查询失败：' + err.message, 'err');
    }
  }
}

async function refreshConfig() {
  try {
    const { body } = await api('/api/v1/config');
    if (body && body.success) {
      const d = body.data;
      $('#cfg-port').textContent = String(d.listen_port ?? d.port ?? '—');
      $('#cfg-version').textContent = d.version || '—';
      const origin = window.location.origin;
      $('#cfg-api').textContent = origin + '/api/v1';
      $('#api-example').textContent =
        'curl -X POST ' + origin + '/api/v1/vm/start \\\n' +
        '  -H "Authorization: Bearer <你的Token>"';
    }
  } catch { /* 配置信息失败不阻塞主流程 */ }
}

async function refreshTokenMeta() {
  try {
    const { body } = await api('/api/v1/auth/token/meta');
    if (body && body.success) {
      $('#token-created').textContent = fmtTime(body.data.created_at);
      $('#token-updated').textContent = fmtTime(body.data.updated_at);
    }
  } catch { /* 忽略 */ }
}

async function refreshLogs() {
  try {
    const { body } = await api('/api/v1/logs?limit=100');
    const list = $('#logs-list');
    const empty = $('#logs-empty');
    list.innerHTML = '';
    const entries = (body && body.success && body.data.entries) || [];
    empty.classList.toggle('hidden', entries.length !== 0);
    for (const e of entries) {
      const li = document.createElement('li');
      li.className = 'log-item';

      const ts = document.createElement('span');
      ts.className = 'mono';
      ts.textContent = fmtTime(e.ts);

      const action = document.createElement('span');
      action.className = 'mono';
      action.textContent = String(e.action || '');

      const result = document.createElement('span');
      const r = String(e.result || '');
      result.className = 'log-result-' + r;
      result.textContent = r;

      const detail = document.createElement('span');
      detail.className = 'log-detail';
      const parts = [];
      if (e.state) parts.push('state=' + e.state);
      if (e.reason) parts.push(String(e.reason));
      if (e.client_ip) parts.push('ip=' + e.client_ip);
      detail.textContent = parts.join(' ');

      li.append(ts, action, result, detail);
      list.appendChild(li);
    }
  } catch { /* 日志加载失败静默 */ }
}

function showLogin(message) {
  $('#setup-view').classList.add('hidden');
  $('#login-view').classList.remove('hidden');
  $('#app-view').classList.add('hidden');
  $('#btn-logout').classList.add('hidden');
  $('#login-error').textContent = message || '';
  if (state.refreshTimer) { clearInterval(state.refreshTimer); state.refreshTimer = null; }
}

function showSetup() {
  $('#setup-view').classList.remove('hidden');
  $('#login-view').classList.add('hidden');
  $('#app-view').classList.add('hidden');
  $('#btn-logout').classList.add('hidden');
  if (state.refreshTimer) { clearInterval(state.refreshTimer); state.refreshTimer = null; }
}

function showApp() {
  $('#setup-view').classList.add('hidden');
  $('#login-view').classList.add('hidden');
  $('#app-view').classList.remove('hidden');
  $('#btn-logout').classList.remove('hidden');
  refreshStatus();
  refreshConfig();
  refreshTokenMeta();
  refreshLogs();
  if (state.refreshTimer) clearInterval(state.refreshTimer);
  // 每 30 秒自动刷新一次状态
  state.refreshTimer = setInterval(refreshStatus, 30000);
}

/* ---------- 操作 ---------- */

async function doStart() {
  if (state.busy) return;
  setBusy(true);
  setActionMessage('正在执行开机…', '');
  try {
    const { status, body } = await api('/api/v1/vm/start', { method: 'POST' });
    const d = body && body.data;
    if (body && body.success) {
      setActionMessage((d && d.message) || '开机指令已执行', 'ok');
      showToast((d && d.message) || '开机指令已执行');
    } else {
      setActionMessage((body && body.error) || (d && d.message) || '开机失败', 'err');
    }
    await refreshStatus();
    await refreshLogs();
  } catch (err) {
    if (err.message !== 'unauthorized') setActionMessage('开机请求失败：' + err.message, 'err');
  } finally {
    setBusy(false);
  }
}

async function doShutdown() {
  if (state.busy) return;
  if (!window.confirm('将向 Windows 发送正常关机信号。\n超时不会强制断电，确定继续？')) return;
  setBusy(true);
  setActionMessage('已发送正常关机指令，正在等待 Windows 关机（最长约 2 分钟）…', '');
  try {
    const { body } = await api('/api/v1/vm/shutdown', { method: 'POST' });
    const d = body && body.data;
    if (body && body.success) {
      setActionMessage((d && d.message) || '虚拟机已正常关机', 'ok');
    } else {
      setActionMessage((body && body.message) || (d && d.message) || (body && body.error) || '关机未完成', 'err');
    }
    showToast((d && d.message) || '关机指令已处理');
    await refreshStatus();
    await refreshLogs();
  } catch (err) {
    if (err.message !== 'unauthorized') setActionMessage('关机请求失败：' + err.message, 'err');
  } finally {
    setBusy(false);
  }
}

async function doRotateToken() {
  if (!window.confirm('重新生成后，旧 Token 立即失效。\n新 Token 仅显示一次，确定继续？')) return;
  try {
    const { body } = await api('/api/v1/auth/token/rotate', { method: 'POST' });
    if (body && body.success) {
      const wrap = $('#new-token-wrap');
      $('#new-token').textContent = body.data.new_token;
      wrap.classList.remove('hidden');
      showToast('Token 已重新生成');
      refreshTokenMeta();
      refreshLogs();
    } else {
      showToast((body && body.error) || 'Token 重新生成失败');
    }
  } catch (err) {
    if (err.message !== 'unauthorized') showToast('请求失败：' + err.message);
  }
}

/* ---------- 事件绑定 ---------- */

$('#login-form').addEventListener('submit', async (ev) => {
  ev.preventDefault();
  const input = $('#token-input');
  const token = input.value.trim();
  if (!token) return;
  state.token = token;
  // 用一个轻量请求验证 Token
  try {
    const { body, status } = await api('/api/v1/auth/token/meta');
    if (status === 200 && body && body.success) {
      sessionStorage.setItem('vmremote_token', token);
      input.value = '';
      $('#login-error').textContent = '';
      showApp();
    } else {
      state.token = '';
      $('#login-error').textContent = 'Token 无效，请检查后重试';
    }
  } catch {
    state.token = '';
    $('#login-error').textContent = '无法连接服务或 Token 无效';
  }
});

$('#btn-logout').addEventListener('click', () => {
  sessionStorage.removeItem('vmremote_token');
  state.token = '';
  showLogin('');
});

$('#btn-setup').addEventListener('click', async () => {
  const btn = $('#btn-setup');
  btn.disabled = true;
  $('#setup-error').textContent = '';
  try {
    const resp = await fetch('/api/v1/setup', { method: 'POST' });
    const body = await resp.json().catch(() => null);
    if (resp.ok && body && body.success) {
      $('#setup-gen-wrap').classList.add('hidden');
      $('#setup-token').textContent = body.data.new_token;
      $('#setup-token-wrap').classList.remove('hidden');
      state.token = body.data.new_token;
    } else {
      $('#setup-error').textContent = (body && body.error) || '初始化失败';
      btn.disabled = false;
    }
  } catch (err) {
    $('#setup-error').textContent = '请求失败：' + err.message;
    btn.disabled = false;
  }
});

$('#btn-copy-setup').addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText($('#setup-token').textContent);
    showToast('已复制到剪贴板');
  } catch {
    showToast('复制失败，请手动选中复制');
  }
});

$('#btn-setup-done').addEventListener('click', () => {
  sessionStorage.setItem('vmremote_token', state.token);
  showApp();
});

$('#btn-refresh').addEventListener('click', refreshStatus);
$('#btn-start').addEventListener('click', doStart);
$('#btn-shutdown').addEventListener('click', doShutdown);
$('#btn-rotate').addEventListener('click', doRotateToken);
$('#btn-copy-token').addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText($('#new-token').textContent);
    showToast('已复制到剪贴板');
  } catch {
    showToast('复制失败，请手动选中复制');
  }
});
$('#btn-logs-refresh').addEventListener('click', refreshLogs);
$('#btn-logs-clear').addEventListener('click', async () => {
  if (!window.confirm('确定清空全部操作日志？')) return;
  try {
    const { body } = await api('/api/v1/logs', { method: 'DELETE' });
    if (body && body.success) { refreshLogs(); showToast('日志已清空'); }
  } catch { showToast('清空失败'); }
});

/* ---------- 启动 ---------- */

(async () => {
  try {
    const resp = await fetch('/api/v1/setup/status');
    const body = await resp.json();
    if (body && body.success && body.data && body.data.setup_required) {
      showSetup();
      return;
    }
  } catch { /* 网络异常时退回登录视图 */ }
  if (state.token) {
    showApp();
  } else {
    showLogin('');
  }
})();
