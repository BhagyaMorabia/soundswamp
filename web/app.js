document.addEventListener('DOMContentLoaded', () => {
    const sessionCodeEl = document.getElementById('session-code');
    const clientCountEl = document.getElementById('client-count');
    const globalDelayEl = document.getElementById('global-delay');
    const deviceListEl = document.getElementById('device-list');
    const qrImageEl = document.getElementById('qr-image');
    const statusDot = document.querySelector('.dot');
    
    const template = document.getElementById('device-card-template');

    // Fetch initial state via HTTP
    function fetchState() {
        fetch('/api/session')
            .then(res => res.json())
            .then(data => {
                if (data.active) {
                    sessionCodeEl.textContent = data.session_id;
                    updateUI(data);
                } else {
                    sessionCodeEl.textContent = 'No active session';
                }
            })
            .catch(err => console.error('Error fetching state:', err));
    }

    // Update UI components with new state
    function updateUI(state) {
        clientCountEl.textContent = state.client_count;
        globalDelayEl.textContent = state.global_delay ? `${state.global_delay.toFixed(1)} ms` : '-- ms';
        
        // Refresh QR code to ensure it's loaded
        if (!qrImageEl.complete || qrImageEl.naturalWidth === 0) {
            qrImageEl.src = '/api/qr?' + new Date().getTime();
        }

        renderDevices(state.clients || []);
    }

    // Render device cards
    function renderDevices(clients) {
        deviceListEl.innerHTML = '';
        
        if (clients.length === 0) {
            deviceListEl.innerHTML = '<div class="glass-panel" style="grid-column: 1/-1; text-align: center; color: var(--text-muted); padding: 2rem;">Waiting for devices to connect...</div>';
            return;
        }

        clients.forEach(client => {
            const clone = template.content.cloneNode(true);
            const card = clone.querySelector('.device-card');
            
            card.dataset.id = client.id;
            clone.querySelector('.device-name').textContent = client.name || 'Unknown Device';
            
            const jitterVal = client.jitter_ms ? client.jitter_ms.toFixed(1) : '--';
            clone.querySelector('.jitter-value').textContent = `${jitterVal} ms`;
            
            const select = clone.querySelector('.channel-select');
            select.value = client.channel;
            select.addEventListener('change', (e) => assignChannel(client.id, parseInt(e.target.value)));
            
            const btnKick = clone.querySelector('.btn-kick');
            btnKick.addEventListener('click', () => kickDevice(client.id));
            
            deviceListEl.appendChild(clone);
        });
    }

    // API Actions
    function assignChannel(clientId, channel) {
        fetch('/api/channel', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ client_id: clientId, channel: channel })
        }).catch(err => console.error('Error assigning channel:', err));
    }

    function kickDevice(clientId) {
        if (!confirm('Kick this device?')) return;
        fetch('/api/kick', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ client_id: clientId })
        }).catch(err => console.error('Error kicking device:', err));
    }

    // Polling fallback since WebSocket isn't fully wired in main.go yet
    function startPolling() {
        setInterval(() => {
            fetchState();
        }, 2000);
    }

    // Initial load
    fetchState();
    startPolling();
});
