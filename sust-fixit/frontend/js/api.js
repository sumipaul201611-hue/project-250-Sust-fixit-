// Thin wrapper around the backend API. Every page loads this first.
const API = {
  base: '', // same origin, since the Go server also serves these static files

  token(){ return localStorage.getItem('fixit_token'); },
  user(){
    try { return JSON.parse(localStorage.getItem('fixit_user') || 'null'); }
    catch { return null; }
  },
  setSession(token, user){
    localStorage.setItem('fixit_token', token);
    localStorage.setItem('fixit_user', JSON.stringify(user));
  },
  clearSession(){
    localStorage.removeItem('fixit_token');
    localStorage.removeItem('fixit_user');
  },
  isLoggedIn(){ return !!this.token(); },
  isAdmin(){ return this.user()?.role === 'admin'; },

  async _handle(res){
    let body = null;
    try { body = await res.json(); } catch { /* no body */ }
    if (!res.ok) {
      const msg = (body && body.error) ? body.error : `Request failed (${res.status})`;
      if (res.status === 401 && this.isLoggedIn()) {
        // token expired/invalid: drop the stale session
        this.clearSession();
      }
      throw new Error(msg);
    }
    return body;
  },

  async get(path){
    const headers = {};
    if (this.token()) headers['Authorization'] = 'Bearer ' + this.token();
    const res = await fetch(this.base + path, { headers });
    return this._handle(res);
  },

  async postJSON(path, data){
    const headers = { 'Content-Type': 'application/json' };
    if (this.token()) headers['Authorization'] = 'Bearer ' + this.token();
    const res = await fetch(this.base + path, { method: 'POST', headers, body: JSON.stringify(data) });
    return this._handle(res);
  },

  async postForm(path, formData, method = 'POST'){
    const headers = {};
    if (this.token()) headers['Authorization'] = 'Bearer ' + this.token();
    const res = await fetch(this.base + path, { method, headers, body: formData });
    return this._handle(res);
  },

  async del(path){
    const headers = {};
    if (this.token()) headers['Authorization'] = 'Bearer ' + this.token();
    const res = await fetch(this.base + path, { method: 'DELETE', headers });
    return this._handle(res);
  },
};

// ---- small shared helpers used across pages ----

function escapeHtml(s){
  return String(s ?? '').replace(/[&<>"']/g, c => ({
    '&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'
  }[c]));
}

function timeAgo(iso){
  const d = new Date(iso);
  const secs = Math.max(1, Math.floor((Date.now() - d.getTime()) / 1000));
  const units = [[31536000,'y'],[2592000,'mo'],[604800,'w'],[86400,'d'],[3600,'h'],[60,'m']];
  for (const [s, label] of units) {
    if (secs >= s) return Math.floor(secs / s) + label + ' ago';
  }
  return 'just now';
}

const STATUS_LABEL = {
  reported: 'Reported', verified: 'Verified', in_progress: 'In progress', resolved: 'Resolved',
};

function statusTag(status){
  return `<span class="status-tag tag-${status}">${STATUS_LABEL[status] || status}</span>`;
}

// Renders the nav bar + auth-aware right side. Called by every page after DOM load.
function renderNav(activePage){
  const nav = document.getElementById('nav-slot');
  if (!nav) return;
  const user = API.user();
  const links = [
    ['index.html', 'Home'],
    ['feed.html', 'Feed'],
  ];
  if (user) links.push(['report.html', 'Report a problem'], ['dashboard.html', 'My reports']);
  if (user?.role === 'admin') links.push(['admin.html', 'Admin']);

  const linkHtml = links.map(([href, label]) =>
    `<a href="${href}" class="${activePage === href ? 'active' : ''}">${label}</a>`
  ).join('');

  const rightHtml = user
    ? `<span class="user-chip">Hi, <b>${escapeHtml(user.name.split(' ')[0])}</b></span>
       <button class="btn btn-ghost btn-sm" id="logout-btn">Log out</button>`
    : `<a href="login.html" class="btn btn-ghost btn-sm">Log in</a>
       <a href="register.html" class="btn btn-primary btn-sm">Sign up</a>`;

  nav.innerHTML = `
    <div class="wrap">
      <a href="index.html" class="brand"><span class="mark">🛠️</span> SUST FixIt</a>
      <nav class="nav">${linkHtml}</nav>
      <div class="nav-right">${rightHtml}</div>
    </div>`;

  document.getElementById('logout-btn')?.addEventListener('click', () => {
    API.clearSession();
    window.location.href = 'index.html';
  });
}

// Guards a page behind login (call at top of page script). Returns the user or redirects.
function requireLogin(){
  if (!API.isLoggedIn()) {
    window.location.href = 'login.html?next=' + encodeURIComponent(location.pathname.split('/').pop());
    return null;
  }
  return API.user();
}

function requireAdmin(){
  const u = requireLogin();
  if (u && u.role !== 'admin') {
    window.location.href = 'index.html';
    return null;
  }
  return u;
}
