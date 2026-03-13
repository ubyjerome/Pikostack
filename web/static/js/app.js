function pikoview() {
  return {
    isDark: true,
    sysStats: { cpu: 0, mem_pct: 0, disk_pct: 0, load: '0.00' },
    wsConnected: false,
    sidebarOpen: false,
    ws: null,

    init() {
      const saved = localStorage.getItem('piko-theme');
      this.isDark = saved !== 'light';
      this.applyTheme();
      this.connectWS();
      this.pollStats();
      setInterval(() => this.pollStats(), 15000);
      // Listen for toast events fired from non-Alpine contexts
      window.addEventListener('toast', (e) => this.showToast(e.detail));
    },

    toggleTheme() {
      this.isDark = !this.isDark;
      localStorage.setItem('piko-theme', this.isDark ? 'dark' : 'light');
      this.applyTheme();
    },

    applyTheme() {
      const root = document.documentElement;
      if (this.isDark) root.classList.add('dark');
      else root.classList.remove('dark');
    },

    connectWS() {
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      try { this.ws = new WebSocket(`${proto}//${location.host}/ws/events`); }
      catch (_) { setTimeout(() => this.connectWS(), 5000); return; }

      this.ws.onopen = () => { this.wsConnected = true; };
      this.ws.onmessage = (e) => {
        try { this.handleEvent(JSON.parse(e.data)); } catch (_) {}
      };
      this.ws.onclose = () => {
        this.wsConnected = false;
        setTimeout(() => this.connectWS(), 3000);
      };
      this.ws.onerror = () => this.ws.close();
    },

    handleEvent(ev) {
      if (ev.type === 'crash' || ev.type === 'error') {
        this.showToast({ msg: `[${ev.type}] ${ev.message}`, type: 'error' });
      } else if (ev.type === 'restart') {
        this.showToast({ msg: 'Auto-restarting service', type: 'warning' });
      }
      // Refresh HTMX service list if visible
      const list = document.getElementById('service-list');
      if (list && typeof htmx !== 'undefined') htmx.trigger(list, 'load');
    },

    async pollStats() {
      try {
        const res = await fetch('/api/v1/system/stats');
        if (!res.ok) return;
        const data = await res.json();
        // Normalize — server returns strings like "57.3", coerce to numbers for progress bars
        this.sysStats = {
          cpu:      parseFloat(data.cpu)      || 0,
          mem_pct:  parseFloat(data.mem_pct)  || 0,
          disk_pct: parseFloat(data.disk_pct) || 0,
          mem_total: data.mem_total || 0,
          mem_used:  data.mem_used  || 0,
          load:     data.load || '0.00',
        };
      } catch (_) {}
    },

    showToast(detail) {
      const container = document.getElementById('toast-container');
      if (!container) return;
      const colors = {
        success: 'bg-emerald-500/10 border-emerald-500/30 text-emerald-300',
        error:   'bg-rose-500/10 border-rose-500/30 text-rose-300',
        warning: 'bg-amber-500/10 border-amber-500/30 text-amber-300',
        info:    'bg-brand-600/10 border-brand-600/30 text-brand-300',
      };
      const cls = colors[detail.type] || colors.info;
      const el = document.createElement('div');
      el.className = `flex items-center gap-3 px-4 py-3 rounded-xl border text-sm font-medium shadow-xl backdrop-blur cursor-pointer ${cls}`;
      el.innerHTML = `<span class="flex-1">${detail.msg}</span>
        <button class="opacity-50 hover:opacity-100 transition flex-shrink-0">
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/></svg>
        </button>`;
      el.onclick = () => el.remove();
      container.appendChild(el);
      setTimeout(() => el.remove(), 5000);
    }
  };
}

// Global helpers callable from any page script
async function serviceAction(id, action) {
  try {
    const res = await fetch(`/api/v1/services/${id}/${action}`, { method: 'POST' });
    const data = await res.json().catch(() => ({}));
    const type = res.ok ? 'success' : 'error';
    const msg  = res.ok ? (data.status || action + 'd') : (data.error || 'Action failed');
    window.dispatchEvent(new CustomEvent('toast', { detail: { msg, type } }));
    if (res.ok) setTimeout(() => location.reload(), 800);
  } catch (e) {
    window.dispatchEvent(new CustomEvent('toast', { detail: { msg: e.message, type: 'error' } }));
  }
}
