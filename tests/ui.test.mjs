import assert from 'node:assert/strict';
import test from 'node:test';
import { escapeHTML, formatTime, healthLabel, monitorStatus } from '../static/js/format.js';
import { apiRequest } from '../static/js/api.js';

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

test('API unwraps auth data and preserves caller cancellation', async () => {
    const original = globalThis.fetch;
    const controller = new AbortController();
    globalThis.fetch = async (_path, options) => {
        assert.equal(options.signal, controller.signal);
        return new Response(JSON.stringify({ code: 200, data: { authenticated: true } }));
    };
    try {
        assert.deepEqual(await apiRequest('/api/auth/check', { signal: controller.signal }), { authenticated: true });
    } finally {
        globalThis.fetch = original;
    }
});
