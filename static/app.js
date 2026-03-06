import { state } from './js/state.js';
import { 
    updateTime, loadDashboardData, fetchZones, fetchMonitors, loadSettings, 
    viewRecords, hideRecords, openRecordModal, openMonitorModal, openAccountModal, 
    editRecord, deleteRecord, editMonitor, deleteMonitor, activateAccount, switchAccount, 
    openRestoreModal, openScheduleSwitchModal, updateAccountSwitcher
} from './js/ui.js';
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
        updateTime();
        setInterval(() => updateTime(), 1000);

        this.initNavigation();

        loadDashboardData();
        fetchZones();
        fetchMonitors();
        loadSettings();

        this.startMonitorPolling();

        bindEvents();
    }

    initNavigation() {
        const sections = ['dashboard', 'domains', 'strategies', 'settings'];
        sections.forEach(section => {
            const navItem = document.getElementById(`nav-${section}`);
            if (navItem) {
                navItem.addEventListener('click', (e) => {
                    e.preventDefault();
                    this.switchSection(section);
                });
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

    switchSection(section) {
        ['dashboard', 'domains', 'strategies', 'settings'].forEach(s => {
            const navItem = document.getElementById(`nav-${s}`);
            const sectionEl = document.getElementById(`section-${s}`);
            
            if (navItem) {
                if (s === section) {
                    navItem.classList.add('active');
                } else {
                    navItem.classList.remove('active');
                }
            }
            
            if (sectionEl) {
                if (s === section) {
                    sectionEl.classList.remove('hidden');
                    sectionEl.classList.add('fade-in');
                } else {
                    sectionEl.classList.add('hidden');
                }
            }
        });
    
        const titles = {
            dashboard: '控制面板',
            domains: '域名管理',
            strategies: '监控策略',
            settings: '系统设置'
        };
        document.getElementById('section-title').textContent = titles[section] || '控制面板';
    
        switch(section) {
            case 'dashboard':
                loadDashboardData();
                break;
            case 'domains':
                updateAccountSwitcher();
                fetchZones();
                break;
            case 'strategies':
                fetchMonitors();
                break;
            case 'settings':
                loadSettings();
                break;
        }
    }
}

let dnsManager;
document.addEventListener('DOMContentLoaded', () => {
    dnsManager = new DNSManager();
    window.dnsManager = dnsManager;
});