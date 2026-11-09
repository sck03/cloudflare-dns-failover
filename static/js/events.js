import { 
    hideMonitorModal, submitMonitorForm, 
    hideRecordModal, submitRecordForm, 
    hideRestoreModal, confirmRestore, 
    hideScheduleSwitchModal, saveScheduleSwitch, 
    hideAccountModal, submitAccountForm 
} from './components/modals.js';
import { saveSettings } from './components/settings.js';
import { showNotification } from './components/notifications.js';

export function bindEvents() {
    const eventMappings = {
        'save-settings': { event: 'click', handler: saveSettings },
        'monitor-modal-close': { event: 'click', handler: hideMonitorModal },
        'monitor-modal-cancel': { event: 'click', handler: hideMonitorModal },
        'monitor-form': { event: 'submit', handler: submitMonitorForm, preventDefault: true },
        'record-modal-close': { event: 'click', handler: hideRecordModal },
        'record-modal-cancel': { event: 'click', handler: hideRecordModal },
        'record-form': { event: 'submit', handler: submitRecordForm, preventDefault: true },
        'restore-modal-close': { event: 'click', handler: hideRestoreModal },
        'restore-modal-cancel': { event: 'click', handler: hideRestoreModal },
        'restore-modal-confirm': { event: 'click', handler: confirmRestore },
        'schedule-modal-close': { event: 'click', handler: hideScheduleSwitchModal },
        'schedule-modal-cancel': { event: 'click', handler: hideScheduleSwitchModal },
        'schedule-modal-save': { event: 'click', handler: saveScheduleSwitch },
        'account-modal-close': { event: 'click', handler: hideAccountModal },
        'account-modal-cancel': { event: 'click', handler: hideAccountModal },
        'account-form': { event: 'submit', handler: submitAccountForm, preventDefault: true },
    };

    for (const id in eventMappings) {
        const element = document.getElementById(id);
        if (element) {
            const { event, handler, preventDefault } = eventMappings[id];
            let pending = false;
            element.addEventListener(event, async (e) => {
                if (preventDefault) e.preventDefault();
                if (pending) return;
                pending = true;
                const button = preventDefault ? element.querySelector('[type="submit"]') : element;
                if (button) button.disabled = true;
                try {
                    await handler();
                } catch (error) {
                    console.error(`Event handler for #${id} failed:`, error);
                    showNotification(error.message || '操作失败', 'error');
                } finally {
                    pending = false;
                    if (button) button.disabled = false;
                }
            });
        }
    }
}
