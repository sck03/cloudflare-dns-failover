// A save belongs to the form session that submitted it. Closing and reopening
// the same record creates a new session, which an older response must not close.
const sessions = new Map();

export function showModal(id) {
    sessions.set(id, Symbol());
    document.getElementById(id)?.classList.remove('hidden');
}

export function hideModal(id) {
    sessions.delete(id);
    document.getElementById(id)?.classList.add('hidden');
}

export function captureModal(id) {
    const session = sessions.get(id);
    return () => session !== undefined && sessions.get(id) === session;
}
