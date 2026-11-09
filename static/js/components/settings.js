import { apiRequest } from '../api.js';
import { showNotification } from './notifications.js';
import { loadAccounts } from './accounts.js';
import { LatestRequest } from '../requests.js';

const settingsRequest = new LatestRequest();

export function cancelSettings() {
    settingsRequest.cancel();
}

export async function loadSettings() {
    try {
        await Promise.all([loadAccounts(), settingsRequest.run('/api/config', config => {
            if (config.dingtalk) {
                document.getElementById('set-ding-enabled').checked = config.dingtalk.enabled || false;
                document.getElementById('set-ding-token').value = config.dingtalk.access_token || '';
                document.getElementById('set-ding-secret').value = config.dingtalk.secret || '';
            }

            if (config.email) {
                document.getElementById('set-email-enabled').checked = config.email.enabled || false;
                document.getElementById('set-email-host').value = config.email.host || '';
                document.getElementById('set-email-port').value = config.email.port || '';
                document.getElementById('set-email-username').value = config.email.username || '';
                document.getElementById('set-email-password').value = config.email.password || '';
                document.getElementById('set-email-to').value = config.email.to || '';
            }

            if (config.telegram) {
                document.getElementById('set-tg-enabled').checked = config.telegram.enabled || false;
                document.getElementById('set-tg-bot-token').value = config.telegram.bot_token || '';
                document.getElementById('set-tg-chat-id').value = config.telegram.chat_id || '';
            }
        })]);
    } catch (error) {
        showNotification(`加载设置失败：${error.message}`, 'error');
    }
}

export async function saveSettings() {
    cancelSettings();
    const config = {
        dingtalk: {
            enabled: document.getElementById('set-ding-enabled').checked,
            access_token: document.getElementById('set-ding-token').value,
            secret: document.getElementById('set-ding-secret').value
        },
        email: {
            enabled: document.getElementById('set-email-enabled').checked,
            host: document.getElementById('set-email-host').value,
            port: Number(document.getElementById('set-email-port').value) || 0,
            username: document.getElementById('set-email-username').value,
            password: document.getElementById('set-email-password').value,
            to: document.getElementById('set-email-to').value
        },
        telegram: {
            enabled: document.getElementById('set-tg-enabled').checked,
            bot_token: document.getElementById('set-tg-bot-token').value,
            chat_id: document.getElementById('set-tg-chat-id').value
        }
    };
    
    try {
        await apiRequest('/api/config', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(config)
        });
        
        showNotification('设置保存成功', 'success');
    } catch (error) {
        console.error('保存设置失败:', error);
        showNotification(`保存失败: ${error.message || error}`, 'error');
    }
}
