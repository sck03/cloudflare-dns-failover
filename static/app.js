import { state } from './js/state.js';
import * as ui from './js/ui.js';
import { bindEvents } from './js/events.js';

class DNSManager {
    constructor() {
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', () => this.checkAuthAndInit());
        } else {
            this.checkAuthAndInit();
        }
    }

    async checkAuthAndInit() {
        try {
            const response = await fetch(`${state.baseURL}/api/auth/check`);
            const result = await response.json();
            
            if (result.code === 200 && result.data) {
                if (result.data.auth_enabled === false) {
                    this.init();
                    return;
                }

                if (result.data.need_setup || !result.data.authenticated) {
                    window.location.href = '/login';
                    return;
                }
            }
            
            this.init();
        } catch (error) {
            console.error('认证检查失败:', error);
            this.init();
        }
    }

    init() {
        Object.assign(this, ui);

        console.log("[APP] Initializing DNSManager...");
        this.updateTime();
        setInterval(() => this.updateTime(), 1000);

        this.initNavigation();

        this.loadDashboardData();
        this.fetchZones();
        this.fetchMonitors();
        this.loadSettings();

        this.startMonitorPolling();

        this.bindViewChange();
        bindEvents();
    }

    initNavigation() {
        const sections = ['dashboard', 'domains', 'strategies', 'settings'];
        sections.forEach(section => {
            const navItem = document.getElementById(`nav-${section}`);
            if (navItem) {
                navItem.addEventListener('click', (e) => {
                    e.preventDefault();
                    console.log(`[APP] Navigation click detected for section: ${section}`);
                    this.switchSection(section);
                });
            }
        });
    }

    bindViewChange() {
        document.addEventListener('viewchanged', (e) => {
            const section = e.detail.section;
            console.log(`[APP] 'viewchanged' event received for section: ${section}`);
            switch(section) {
                case 'dashboard':
                    this.loadDashboardData();
                    break;
                case 'domains':
                    this.updateAccountSwitcher();
                    this.fetchZones();
                    break;
                case 'strategies':
                    this.fetchMonitors();
                    break;
                case 'settings':
                    this.loadSettings();
                    break;
            }
        });
    }

    startMonitorPolling() {
        state.monitorInterval = setInterval(() => {
            if (document.getElementById('section-dashboard') && 
                !document.getElementById('section-dashboard').classList.contains('hidden')) {
                loadDashboardData();
            }
        }, 30000);
    }


}

let dnsManager;
document.addEventListener('DOMContentLoaded', () => {
    dnsManager = new DNSManager();
    window.dnsManager = dnsManager;
});