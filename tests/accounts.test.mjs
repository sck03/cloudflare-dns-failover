import assert from 'node:assert/strict';
import test from 'node:test';
import { loadAccounts, editAccount, deleteAccount, activateAccount, refreshAccountViews } from '../static/js/components/accounts.js';
import { submitAccountForm, openAccountModal, hideAccountModal, editRecord, hideRecordModal, submitRecordForm } from '../static/js/components/modals.js';
import { loadSettings, saveSettings, cancelSettings } from '../static/js/components/settings.js';
import { viewRecords } from '../static/js/components/domains.js';
import { state } from '../static/js/state.js';

function mockDocument(t) {
    const original = globalThis.document;
    const elements = new Map();
    function element() {
        const classes = new Set();
        return {
            value: '', checked: false, innerHTML: '', textContent: '', children: [], style: {},
            classList: { add: v => classes.add(v), remove: v => classes.delete(v), contains: v => classes.has(v) },
            replaceChildren() { this.innerHTML = ''; },
            appendChild(child) { this.children.push(child); },
            setAttribute() {}
        };
    }
    globalThis.document = {
        getElementById(id) {
            if (!elements.has(id)) elements.set(id, element());
            return elements.get(id);
        },
        createElement: element
    };
    t.mock.method(globalThis, 'setTimeout', () => 0);
    t.after(() => { cancelSettings(); globalThis.document = original; });
    return id => document.getElementById(id);
}

test('one latest account response updates the list, switcher and editor', async t => {
    const element = mockDocument(t);
    const pending = [];
    t.mock.method(globalThis, 'fetch', (_path, options) => new Promise(resolve => pending.push({ resolve, signal: options.signal })));
    const first = loadAccounts();
    const second = loadAccounts();
    assert.equal(pending[0].signal.aborted, true);
    pending[1].resolve(Response.json({ accounts: [{ id: 'new', name: 'new', has_api_token: true }] }));
    await second;
    pending[0].resolve(Response.json({ accounts: [{ id: 'old', name: 'old' }] }));
    await first;
    assert.equal(element('current-account-name').textContent, 'new');
    assert.match(element('cf-accounts-list').innerHTML, /data-account-id="new"/);
    assert.match(element('account-switcher').innerHTML, /value="new"/);
    assert.doesNotMatch(element('account-switcher').innerHTML, /value="old"/);
    editAccount('new');
    assert.equal(state.editingAccountId, 'new');
    assert.equal(element('account-name').value, 'new');
    assert.equal(pending.length, 2, 'editing should use the displayed account snapshot');
});

test('account refresh cancels old DNS reads and clears record editing state', async t => {
    const element = mockDocument(t);
    let oldRecord;
    const paths = [];
    t.mock.method(globalThis, 'fetch', (path, options) => {
        paths.push(path);
        if (path.includes('/records')) return new Promise(resolve => { oldRecord = { resolve, signal: options.signal }; });
        return Promise.resolve(Response.json(path === '/api/zones' ? [] : { accounts: [] }));
    });
    const old = viewRecords('old-zone', 'old.example');
    state.editingRecordId = 'old-record';
    await refreshAccountViews();
    assert.equal(oldRecord.signal.aborted, true);
    oldRecord.resolve(Response.json([{ id: 'old-record' }]));
    await old;
    assert.equal(state.currentZoneId, null);
    assert.equal(state.editingRecordId, null);
    assert.deepEqual(state.recordsCache, []);
    assert.equal(element('current-account-name').textContent, '未配置凭证');
    assert.equal(element('record-modal').classList.contains('hidden'), true);
    assert.equal(paths.filter(path => path === '/api/cloudflare-accounts').length, 1);
});

test('create, edit, activate and delete all refresh account and domain views once', async t => {
    for (const action of ['create', 'edit', 'activate', 'delete']) {
        await t.test(action, async t => {
            const element = mockDocument(t);
            const previousConfirm = globalThis.confirm;
            globalThis.confirm = () => true;
            t.after(() => { globalThis.confirm = previousConfirm; });
            const paths = [];
            t.mock.method(globalThis, 'fetch', async (path, options) => {
                paths.push([path, options.method || 'GET']);
                if (options.method) return Response.json({ message: 'saved' });
                if (path === '/api/zones') return Response.json([]);
                return Response.json({ accounts: action === 'delete' ? [] : [{ id: 'saved', name: 'saved' }] });
            });
            state.currentZoneId = 'obsolete-zone';
            state.recordsCache = [{ id: 'obsolete-record' }];
            element('account-name').value = 'saved';
            element('account-token').value = 'new-token';
            state.editingAccountId = action === 'edit' ? 'old' : null;
            if (action === 'delete') await deleteAccount('old');
            else if (action === 'activate') await activateAccount('saved');
            else await submitAccountForm();
            assert.equal(element('current-account-name').textContent, action === 'delete' ? '未配置凭证' : 'saved');
            assert.equal(state.currentZoneId, null);
            assert.deepEqual(state.recordsCache, []);
            assert.equal(paths.filter(([path, method]) => path === '/api/cloudflare-accounts' && method === 'GET').length, 1);
            assert.equal(paths.filter(([path]) => path === '/api/zones').length, 1);
            assert.equal(paths.length, 3);
        });
    }
});

test('saving notifications cancels older settings reads and sends no account credentials', async t => {
    const element = mockDocument(t);
    let pending, saved;
    t.mock.method(globalThis, 'fetch', (path, options) => {
        if (options.method === 'POST') {
            saved = JSON.parse(options.body);
            return Promise.resolve(Response.json({ message: 'saved' }));
        }
        if (path === '/api/config') return new Promise(resolve => { pending = { resolve, signal: options.signal }; });
        return Promise.resolve(Response.json({ accounts: [] }));
    });
    const loading = loadSettings();
    element('set-tg-chat-id').value = 'new-chat';
    await saveSettings();
    assert.equal(pending.signal.aborted, true);
    pending.resolve(Response.json({ telegram: { chat_id: 'old-chat' } }));
    await loading;
    assert.equal(element('set-tg-chat-id').value, 'new-chat');
    assert.equal(saved.telegram.chat_id, 'new-chat');
    assert.equal(Object.hasOwn(saved, 'cloudflare'), false);
});

test('an older account save cannot close a reopened editor for the same account', async t => {
    const element = mockDocument(t);
    let finishSave;
    t.mock.method(globalThis, 'fetch', (path, options) => {
        if (options.method === 'PUT') return new Promise(resolve => { finishSave = resolve; });
        return Promise.resolve(Response.json(path === '/api/zones' ? [] : { accounts: [{ id: 'same', name: 'same' }] }));
    });
    openAccountModal({ id: 'same', name: 'same' });
    const saving = submitAccountForm();
    hideAccountModal();
    openAccountModal({ id: 'same', name: 'new draft' });
    finishSave(Response.json({ message: 'saved' }));
    await saving;
    assert.equal(state.editingAccountId, 'same');
    assert.equal(element('account-modal').classList.contains('hidden'), false);
    assert.equal(element('account-name').value, 'new draft');
    hideAccountModal();
});

test('an older DNS save preserves the new editor and does not fetch the new zone', async t => {
    const element = mockDocument(t);
    const paths = [];
    let finishSave;
    t.mock.method(globalThis, 'fetch', (path, options) => {
        paths.push(path);
        if (options.method === 'PUT') return new Promise(resolve => { finishSave = resolve; });
        return Promise.resolve(Response.json([]));
    });
    state.currentZoneId = 'first';
    state.recordsCache = [{ id: 'one', type: 'A', name: 'one.example', content: '192.0.2.1' }];
    editRecord('one');
    const saving = submitRecordForm();
    hideRecordModal();
    state.currentZoneId = 'second';
    state.recordsCache = [{ id: 'two', type: 'A', name: 'two.example', content: '192.0.2.2' }];
    editRecord('two');
    finishSave(Response.json({ id: 'one' }));
    await saving;
    assert.deepEqual(paths, ['/api/zones/first/records/one']);
    assert.equal(state.editingRecordId, 'two');
    assert.equal(element('record-modal').classList.contains('hidden'), false);
    assert.equal(element('record-name').value, 'two.example');
    hideRecordModal();
});
