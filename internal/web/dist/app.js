let privacyMode = false;

function togglePrivacy() {
  privacyMode = !privacyMode;
  document.body.classList.toggle('privacy-mode', privacyMode);
  document.getElementById('privacyBtn').classList.toggle('active', privacyMode);
}

document.getElementById('privacyBtn').addEventListener('click', togglePrivacy);
window.addEventListener('keydown', (e) => {
  if (e.key === 'p' || e.key === 'P') {
    if (document.activeElement.tagName !== 'INPUT') {
      togglePrivacy();
    }
  }
});

async function loadStatus() {
  try {
    const res = await fetch('/api/status');
    const data = await res.json();
    renderDashboard(data);
  } catch (err) {
    console.error('Failed to load status', err);
  }
}

function renderDashboard(data) {
  document.getElementById('accountCount').textContent = data.accounts.length + ' accounts';
  document.getElementById('totalRequestsValue').textContent = data.totalRequests;
  document.getElementById('totalFailoversValue').textContent = data.totalFailovers;

  const readyCount = data.accounts.filter(a => a.status === 'active' && !a.quota.exceeded).length;
  document.getElementById('poolHealthValue').textContent = readyCount + ' / ' + data.accounts.length + ' Ready';

  const activeAcc = data.accounts.find(a => a.isActive);
  document.getElementById('activeAccountName').textContent = activeAcc ? activeAcc.alias : 'None';

  const grid = document.getElementById('accountsGrid');
  grid.innerHTML = '';

  data.accounts.forEach(acc => {
    const card = document.createElement('div');
    card.className = 'account-card ' + (acc.isActive ? 'active ' : '') + (acc.status === 'exhausted' ? 'exhausted' : '');

    const fiveHour = acc.quota.fiveHour || { used: 0, cap: 3 };
    const fiveHourPct = fiveHour.cap > 0 ? Math.min(100, Math.round((fiveHour.used / fiveHour.cap) * 100)) : 0;
    const weekly = acc.quota.weekly || { used: 0, cap: 6 };
    const weeklyPct = weekly.cap > 0 ? Math.min(100, Math.round((weekly.used / weekly.cap) * 100)) : 0;

    let badgeClass = 'badge-ready';
    let badgeText = 'READY';
    if (acc.isActive) {
      badgeClass = 'badge-active';
      badgeText = 'ACTIVE';
    } else if (acc.status === 'exhausted' || acc.quota.exceeded) {
      badgeClass = 'badge-exhausted';
      badgeText = 'EXHAUSTED';
    }

    card.innerHTML = `
      <div class="card-top">
        <div class="account-title">
          <span class="account-alias">${acc.alias}</span>
          <span class="account-email">${acc.email || acc.userName}</span>
        </div>
        <span class="badge ${badgeClass}">${badgeText}</span>
      </div>

      <div class="quota-row">
        <div class="quota-label-row">
          <span>5-Hour Window Limit</span>
          <span>${fiveHour.used.toFixed(2)} / ${fiveHour.cap.toFixed(2)} (${fiveHourPct}%)</span>
        </div>
        <div class="progress-bar-bg">
          <div class="progress-bar-fill ${fiveHourPct > 90 ? 'progress-red' : fiveHourPct > 70 ? 'progress-orange' : 'progress-blue'}" style="width: ${fiveHourPct}%;"></div>
        </div>
      </div>

      <div class="quota-row">
        <div class="quota-label-row">
          <span>Weekly Limit</span>
          <span>${weekly.used.toFixed(2)} / ${weekly.cap.toFixed(2)} (${weeklyPct}%)</span>
        </div>
        <div class="progress-bar-bg">
          <div class="progress-bar-fill ${weeklyPct > 90 ? 'progress-red' : weeklyPct > 70 ? 'progress-orange' : 'progress-blue'}" style="width: ${weeklyPct}%;"></div>
        </div>
      </div>

      <div class="card-actions">
        ${!acc.isActive ? `<button onclick="switchAccount('${acc.id}')" class="btn btn-outline">Set Active</button>` : ''}
        <button onclick="deleteAccount('${acc.id}')" class="btn btn-outline" style="color: #F87171;">Remove</button>
      </div>
    `;
    grid.appendChild(card);
  });
}

async function switchAccount(id) {
  try {
    await fetch('/api/accounts/switch', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id })
    });
    loadStatus();
  } catch (err) {
    alert('Failed to switch: ' + err.message);
  }
}

async function deleteAccount(id) {
  if (!confirm('Remove this account from the pool?')) return;
  try {
    await fetch('/api/accounts/' + id, { method: 'DELETE' });
    loadStatus();
  } catch (err) {
    alert('Failed to delete: ' + err.message);
  }
}

document.getElementById('refreshBtn').addEventListener('click', async () => {
  const btn = document.getElementById('refreshBtn');
  btn.disabled = true;
  btn.textContent = 'Syncing...';
  try {
    await fetch('/api/accounts/refresh', { method: 'POST' });
    await loadStatus();
  } finally {
    btn.disabled = false;
    btn.innerHTML = '<span class="icon">↻</span> Refresh Quotas';
  }
});

// Modal handlers
const modal = document.getElementById('accountModal');
document.getElementById('addAccountBtn').addEventListener('click', () => modal.classList.remove('hidden'));
document.getElementById('closeModalBtn').addEventListener('click', () => modal.classList.add('hidden'));
document.getElementById('cancelModalBtn').addEventListener('click', () => modal.classList.add('hidden'));

document.getElementById('addAccountForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const alias = document.getElementById('aliasInput').value.trim();
  const apiKey = document.getElementById('apiKeyInput').value.trim();

  try {
    const res = await fetch('/api/accounts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ alias, apiKey })
    });
    if (!res.ok) {
      const err = await res.text();
      alert('Error: ' + err);
      return;
    }
    modal.classList.add('hidden');
    document.getElementById('addAccountForm').reset();
    loadStatus();
  } catch (err) {
    alert('Failed to save account: ' + err.message);
  }
});

// SSE event stream
function initSSE() {
  const evtSource = new EventSource('/api/events');
  const term = document.getElementById('terminalLog');

  evtSource.onmessage = (e) => {
    try {
      const event = JSON.parse(e.data);
      const row = document.createElement('div');
      let typeClass = 'system';
      if (event.type === 'failover.success') typeClass = 'switched';
      if (event.type === 'quota.exhausted') typeClass = 'exhausted';
      if (event.type === 'quota.refreshed') typeClass = 'refreshed';

      row.className = 'log-entry ' + typeClass;
      const ts = new Date(event.timestamp).toLocaleTimeString();
      row.textContent = '[' + ts + '] ' + event.message;
      term.appendChild(row);
      term.scrollTop = term.scrollHeight;

      if (event.type === 'failover.success' || event.type === 'quota.refreshed' || event.type === 'account.switched') {
        loadStatus();
      }
    } catch (err) {}
  };
}

loadStatus();
initSSE();
