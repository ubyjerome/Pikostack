// Pikoview client — Alpine.js app + WebSocket event stream

function pikoview() {
  return {
    isDark: true,
    sysStats: { cpu: '0', mem_pct: '0', disk_pct: '0', load: '0' },
    ws: null,
    wsReady: false,

    init() {
      // Theme from localStorage
      const saved = localStorage.getItem('piko-theme');
      this.isDark = saved !== 'light';
      this.applyTheme();

      // Connect global WS event stream
      this.connectWS();

      // Poll system stats every 15s
      this.pollStats();
      setInterval(() => this.pollStats(), 2000);
    },

    toggleTheme() {
      this.isDark = !this.isDark;
      localStorage.setItem('piko-theme', this.isDark ? 'dark' : 'light');
      this.applyTheme();
    },

    applyTheme() {
      const root = document.getElementById('html-root');
      if (this.isDark) {
        root.classList.add('dark');
        document.body.classList.add('bg-slate-950', 'text-slate-100');
        document.body.classList.remove('bg-gray-50', 'text-gray-900');
      } else {
        root.classList.remove('dark');
        document.body.classList.remove('bg-slate-950', 'text-slate-100');
        document.body.classList.add('bg-gray-50', 'text-gray-900');
      }
    },

    connectWS() {
      const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
      this.ws = new WebSocket(`${proto}//${location.host}/ws/events`);

      const dot = document.getElementById('ws-dot');
      const label = document.getElementById('ws-label');

      this.ws.onopen = () => {
        this.wsReady = true;
        if (dot) dot.className = 'w-1.5 h-1.5 rounded-full bg-emerald-400';
        if (label) label.textContent = 'live';
      };

      this.ws.onmessage = (e) => {
        try {
          const ev = JSON.parse(e.data);
          this.handleEvent(ev);
        } catch (_) {}
      };

      this.ws.onclose = () => {
        this.wsReady = false;
        if (dot) dot.className = 'w-1.5 h-1.5 rounded-full bg-slate-600';
        if (label) label.textContent = 'reconnecting…';
        setTimeout(() => this.connectWS(), 100);
      };

      this.ws.onerror = () => this.ws.close();
    },

    handleEvent(ev) {
      // Show toast for crashes and restarts
      if (ev.type === 'crash' || ev.type === 'error') {
        this.showToast({ msg: `[${ev.type}] ${ev.message}`, type: 'error' });
      } else if (ev.type === 'restart') {
        this.showToast({ msg: `Auto-restarting service`, type: 'warning' });
      }

      // Refresh HTMX targets if on dashboard
      if (document.getElementById('service-list')) {
        htmx.trigger('#service-list', 'refresh');
      }
    },

    async pollStats() {
      try {
        const res = await fetch('/api/v1/system/stats');
        if (res.ok) {
          this.sysStats = await res.json();
        }
      } catch (_) {}
    },

    showToast(detail) {
      const container = document.getElementById('toast-container');
      if (!container) return;

      const toast = document.createElement('div');
      const colors = {
        success: 'bg-emerald-500/10 border-emerald-500/20 text-emerald-300',
        error:   'bg-rose-500/10 border-rose-500/20 text-rose-300',
        warning: 'bg-amber-500/10 border-amber-500/20 text-amber-300',
        info:    'bg-brand-600/10 border-brand-600/20 text-brand-300',
      };
      const colorClass = colors[detail.type] || colors.info;

      toast.className = `flex items-center gap-3 px-4 py-3 rounded-xl border text-sm font-medium shadow-lg
                         animate-fade-in backdrop-blur cursor-pointer ${colorClass}`;
      toast.innerHTML = `
        <span>${detail.msg}</span>
        <button class="ml-auto opacity-60 hover:opacity-100 transition">
          <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
          </svg>
        </button>`;
      toast.onclick = () => toast.remove();
      container.appendChild(toast);
      setTimeout(() => toast.remove(), 5000);
    }
  };
}

// Global service action helper (used by service pages)
async function serviceAction(id, action) {
  try {
    const res = await fetch(`/api/v1/services/${id}/${action}`, { method: 'POST' });
    const data = await res.json();
    const type = res.ok ? 'success' : 'error';
    const msg = res.ok ? (data.status || action) : (data.error || 'Action failed');
    window.dispatchEvent(new CustomEvent('toast', { detail: { msg, type } }));
    if (res.ok && typeof htmx !== 'undefined') {
      setTimeout(() => {
        const list = document.getElementById('service-list');
        if (list) htmx.trigger(list, 'load');
      }, 500);
    }
  } catch (e) {
    window.dispatchEvent(new CustomEvent('toast', { detail: { msg: e.message, type: 'error' } }));
  }
}

// Listen to dispatched toast events from non-Alpine contexts
window.addEventListener('toast', (e) => {
  const app = document.body._x_dataStack?.[0];
  if (app?.showToast) {
    app.showToast(e.detail);
  } else {
    // Fallback: find Alpine component
    const el = document.querySelector('[x-data]');
    if (el?.__x) {
      el.__x.$data.showToast(e.detail);
    }
  }
});
