import { state } from './js/state.js';
import { apiRequest } from './js/api.js';
import * as ui from './js/ui.js';
import { bindEvents } from './js/events.js';

class DNSManager {
    constructor() {
        Object.assign(this, ui);
        this.refreshing = false;
    }

    async start() {
        try {
            const auth = await apiRequest('/api/auth/check');
            if (auth.auth_enabled && (auth.need_setup || !auth.authenticated)) {
                window.location.href = '/login';
                return;
            }
            bindEvents();
            for (const section of ['dashboard', 'domains', 'strategies', 'settings']) {
                document.getElementById(`nav-${section}`)?.addEventListener('click', event => {
                    event.preventDefault();
                    this.switchSection(section);
                });
            }
            document.addEventListener('viewchanged', async event => {
                const section = event.detail.section;
                if (section === 'domains') {
                    await this.updateAccountSwitcher();
                    await this.fetchZones();
                } else if (section === 'settings') {
                    await this.loadSettings();
                } else {
                    await this.refreshVisible();
                }
            });
            document.addEventListener('visibilitychange', () => this.refreshVisible());
            this.updateTime();
            setInterval(() => { if (!document.hidden) this.updateTime(); }, 1000);
            state.monitorInterval = setInterval(() => this.refreshVisible(), 30000);
            await Promise.all([this.fetchZones(), this.updateAccountSwitcher(), this.refreshVisible()]);
        } catch (error) {
            this.showNotification(`初始化失败：${error.message}`, 'error');
        }
    }

    async refreshVisible() {
        if (this.refreshing || document.hidden) return;
        this.refreshing = true;
        try {
            if (!document.getElementById('section-dashboard').classList.contains('hidden')) {
                await this.loadDashboardData();
            } else if (!document.getElementById('section-strategies').classList.contains('hidden')) {
                await this.fetchMonitors();
            }
        } finally {
            this.refreshing = false;
        }
    }
}

window.dnsManager = new DNSManager();
window.dnsManager.start();
