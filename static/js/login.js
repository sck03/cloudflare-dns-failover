import { apiRequest } from './api.js';

const form = document.getElementById('login-form');
const errorMessage = document.getElementById('error-message');

function showError(error) {
    errorMessage.textContent = error.message;
    errorMessage.classList.remove('hidden');
}

apiRequest('/api/auth/check').then(auth => {
    if (!auth.auth_enabled || auth.authenticated) window.location.href = '/';
}).catch(showError);

form.addEventListener('submit', async event => {
    event.preventDefault();
    const button = form.querySelector('button');
    if (button.disabled) return;
    button.disabled = true;
    button.textContent = '登录中…';
    errorMessage.classList.add('hidden');
    try {
        await apiRequest('/api/auth/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ token: document.getElementById('token').value }),
        });
        window.location.href = '/';
    } catch (error) {
        showError(error);
    } finally {
        button.disabled = false;
        button.textContent = '登录';
    }
});
