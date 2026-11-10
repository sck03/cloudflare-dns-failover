import assert from 'node:assert/strict';
import test from 'node:test';
import { escapeHTML, formatTime, healthLabel, monitorStatus } from '../static/js/format.js';
import { apiRequest } from '../static/js/api.js';
import { LatestRequest } from '../static/js/requests.js';
import { statusRequest } from '../static/js/status.js';
import { openMonitorModal, hideMonitorModal } from '../static/js/components/modals.js';
import { state } from '../static/js/state.js';
import { fetchMonitors, refreshMonitorViews } from '../static/js/components/monitors.js';

test('unknown, failed and pending DNS states are never labelled healthy', () => {
    for (const status of ['Unknown', 'Error', 'Down', 'Degraded', 'Failover', 'unexpected']) {
        assert.notEqual(monitorStatus({ status }).className, 'status-normal');
    }
    assert.equal(monitorStatus({ status: 'Normal' }).className, 'status-normal');
    assert.equal(healthLabel(null), '待检测');
    assert.equal(healthLabel(false), '不可用');
});

test('domain record text and monitor errors cannot inject HTML', () => {
    assert.equal(escapeHTML('<img src=x onerror="alert(1)"> & \'x\''), '&lt;img src=x onerror=&quot;alert(1)&quot;&gt; &amp; &#39;x&#39;');
});

test('zero Go timestamps are displayed as not yet checked', () => {
    assert.equal(formatTime('0001-01-01T00:00:00Z'), '尚无记录');
    assert.equal(formatTime(null), '尚无记录');
});

test('API displays the backend failure reason instead of a generic status', async () => {
    const original = globalThis.fetch;
    globalThis.fetch = async () => new Response(JSON.stringify({ error: 'DNS switch failed: permission denied' }), { status: 502 });
    try {
        await assert.rejects(apiRequest('/api/monitors/1/restore'), /DNS switch failed: permission denied/);
    } finally {
        globalThis.fetch = original;
    }
});

test('API returns auth data and preserves caller cancellation', async () => {
    const original = globalThis.fetch;
    const controller = new AbortController();
    let requestSignal;
    globalThis.fetch = async (_path, options) => {
        requestSignal = options.signal;
        assert.equal(options.signal.aborted, false);
        return new Response(JSON.stringify({ authenticated: true }));
    };
    try {
        assert.deepEqual(await apiRequest('/api/auth/check', { signal: controller.signal }), { authenticated: true });
        controller.abort();
        assert.equal(requestSignal.aborted, true);
    } finally {
        globalThis.fetch = original;
    }
});

test('invalid JSON on a successful response is reported, not returned as null', async t => {
    t.mock.method(globalThis, 'fetch', async () => new Response('<html>proxy error</html>'));
    await assert.rejects(apiRequest('/api/status'), /JSON/);
});

test('cancellation while reading JSON remains an abort error', async t => {
    const controller = new AbortController();
    t.mock.method(globalThis, 'fetch', async () => ({
        ok: true, status: 200,
        json: async () => { controller.abort(); throw new DOMException('Aborted', 'AbortError'); }
    }));
    await assert.rejects(apiRequest('/api/status', { signal: controller.signal }), { name: 'AbortError' });
});

test('late responses cannot overwrite the newest view or a canceled view', async t => {
    const pending = [];
    t.mock.method(globalThis, 'fetch', (path, options) => new Promise(resolve => pending.push({ path, signal: options.signal, resolve })));
    const request = new LatestRequest();
    const applied = [];
    const first = request.run('/first', value => applied.push(value));
    const second = request.run('/second', value => applied.push(value));
    assert.equal(pending[0].signal.aborted, true);
    pending[1].resolve(new Response(JSON.stringify('second')));
    await second;
    pending[0].resolve(new Response(JSON.stringify('first')));
    await first;
    assert.deepEqual(applied, ['second']);
    const third = request.run('/third', value => applied.push(value));
    request.cancel();
    pending[2].resolve(new Response(JSON.stringify('third')));
    await third;
    assert.deepEqual(applied, ['second']);
});

test('login rejection reports the error without redirecting the login form', async t => {
    t.mock.method(globalThis, 'fetch', async () => new Response(JSON.stringify({ error: 'invalid login password' }), { status: 401 }));
    await assert.rejects(apiRequest('/api/auth/login'), /invalid login password/);
});

test('status refresh honors navigation cancellation and supersedes older polls', async t => {
    const pending = [];
    t.mock.method(globalThis, 'fetch', () => new Promise(resolve => pending.push(resolve)));
    const applied = [];
    const controller = new AbortController();
    const old = statusRequest.run('/api/status', value => applied.push(value), controller.signal);
    controller.abort();
    const fresh = statusRequest.run('/api/status', value => applied.push(value));
    pending[1](new Response(JSON.stringify('after edit')));
    await fresh;
    pending[0](new Response(JSON.stringify('old poll')));
    await old;
    assert.deepEqual(applied, ['after edit']);
});

test('monitor editor ignores old account responses and stays closed after cancellation', async t => {
    const pending = [];
    t.mock.method(globalThis, 'fetch', (_path, options) => new Promise(resolve => pending.push({ resolve, signal: options.signal })));
    const elements = new Map();
    const previousDocument = globalThis.document;
    globalThis.document = {
        getElementById(id) {
            if (!elements.has(id)) {
                const classes = new Set(['hidden']);
                elements.set(id, {
                    classList: { add: value => classes.add(value), remove: value => classes.delete(value), contains: value => classes.has(value) },
                    appendChild() {}, value: '', innerHTML: '', textContent: ''
                });
            }
            return elements.get(id);
        },
        createElement: () => ({})
    };
    t.after(() => { hideMonitorModal(); globalThis.document = previousDocument; });
    const first = openMonitorModal({ id: 1, name: 'old', account_name: 'test' });
    const second = openMonitorModal({ id: 2, name: 'new', account_name: 'test' });
    assert.equal(pending[0].signal.aborted, true);
    pending[1].resolve(Response.json({ accounts: [{ name: 'test' }] }));
    await second;
    pending[0].resolve(Response.json({ accounts: [{ name: 'test' }] }));
    await first;
    assert.equal(state.editingMonitorId, 2);
    assert.equal(elements.get('monitor-name').value, 'new');
    const third = openMonitorModal({ id: 3, name: 'closed' });
    hideMonitorModal();
    pending[2].resolve(Response.json({ accounts: [] }));
    await third;
    assert.equal(state.editingMonitorId, null);
    assert.equal(elements.get('monitor-modal').classList.contains('hidden'), true);
});

test('mutation refresh updates both views from a single status response', async t => {
    let calls = 0;
    t.mock.method(globalThis, 'fetch', async () => {
        calls++;
        return Response.json({ monitors: [], history: [], offline_hot: [], system: { goroutines: 12 } });
    });
    const elements = new Map();
    const previousDocument = globalThis.document;
    globalThis.document = { getElementById(id) {
        if (!elements.has(id)) elements.set(id, {});
        return elements.get(id);
    } };
    t.after(() => { globalThis.document = previousDocument; });
    await refreshMonitorViews();
    assert.equal(calls, 1);
    assert.match(elements.get('strategy-list').innerHTML, /暂无监控策略/);
    assert.match(elements.get('dashboard-monitor-list').innerHTML, /暂无监控策略/);
    assert.equal(elements.get('stat-goroutines').textContent, 12);
});

test('restore remains available when primary is active but backup is preferred', async t => {
    const elements = new Map();
    const previousDocument = globalThis.document;
    globalThis.document = { getElementById(id) {
        if (!elements.has(id)) elements.set(id, {});
        return elements.get(id);
    } };
    t.after(() => { globalThis.document = previousDocument; });
    const monitor = { id: 1, type: 'https', original_ip: '192.0.2.1', backup_ip: '192.0.2.2',
        current_ip: '192.0.2.1', preferred_ip: '192.0.2.2', status: 'Failover' };
    t.mock.method(globalThis, 'fetch', async () => Response.json({ monitors: [monitor] }));
    await fetchMonitors();
    assert.match(elements.get('strategy-list').innerHTML, /openRestoreModal\(1\)/);
    monitor.preferred_ip = monitor.original_ip;
    await fetchMonitors();
    assert.doesNotMatch(elements.get('strategy-list').innerHTML, /openRestoreModal/);
});
