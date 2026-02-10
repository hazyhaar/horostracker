// horostracker — vanilla JS frontend
(function () {
  'use strict';

  const API = '/api';
  let token = localStorage.getItem('ht_token');
  let currentUser = null;

  // --- Router ---
  function route() {
    const hash = location.hash.slice(1) || '/';
    const app = document.getElementById('app');
    app.innerHTML = '';

    if (hash === '/') return renderHome(app);
    if (hash === '/login') return renderLogin(app);
    if (hash === '/register') return renderRegister(app);
    if (hash.startsWith('/q/')) return renderTree(app, hash.slice(3));
    if (hash.startsWith('/u/')) return renderProfile(app, hash.slice(3));
    if (hash === '/ask') return renderAskForm(app);
    if (hash === '/bench') return renderBench(app);

    app.innerHTML = '<div class="empty-state"><h3>Page not found</h3></div>';
  }

  window.addEventListener('hashchange', route);
  window.addEventListener('load', () => {
    updateHeader();
    route();
  });

  // --- API helpers ---
  async function apiFetch(path, opts = {}) {
    const headers = { 'Content-Type': 'application/json', ...opts.headers };
    if (token) headers['Authorization'] = 'Bearer ' + token;
    const resp = await fetch(API + path, { ...opts, headers });
    const data = await resp.json();
    if (!resp.ok) throw new Error(data.error || 'Request failed');
    return data;
  }

  // --- Toast ---
  function toast(msg, type = 'info') {
    let container = document.querySelector('.toast-container');
    if (!container) {
      container = document.createElement('div');
      container.className = 'toast-container';
      document.body.appendChild(container);
    }
    const el = document.createElement('div');
    el.className = 'toast ' + type;
    el.textContent = msg;
    container.appendChild(el);
    setTimeout(() => el.remove(), 3000);
  }

  // --- Header ---
  function updateHeader() {
    const actions = document.getElementById('header-actions');
    if (!actions) return;

    if (token) {
      apiFetch('/me').then(user => {
        currentUser = user;
        actions.innerHTML = `
          <span class="header-user">${esc(user.handle)} · ${user.reputation} rep</span>
          <button class="btn btn-sm" onclick="location.hash='#/u/${esc(user.handle)}'">Profile</button>
          <button class="btn btn-sm" id="logout-btn">Logout</button>
        `;
        document.getElementById('logout-btn').onclick = () => {
          token = null; currentUser = null;
          localStorage.removeItem('ht_token');
          updateHeader();
          location.hash = '#/';
        };
      }).catch(() => {
        token = null; localStorage.removeItem('ht_token');
        updateHeader();
      });
    } else {
      currentUser = null;
      actions.innerHTML = `
        <button class="btn btn-sm" onclick="location.hash='#/login'">Login</button>
        <button class="btn btn-sm btn-primary" onclick="location.hash='#/register'">Register</button>
      `;
    }
  }

  // --- Pages ---

  async function renderHome(app) {
    app.innerHTML = `
      <div class="search-wrap">
        <input class="search-input" id="search-input" placeholder="Search questions, topics, evidence..." autofocus>
        <button class="btn btn-primary" id="search-btn">Search</button>
      </div>
      <div id="tags-cloud" class="tags-cloud"></div>
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px">
        <h2>Hot Questions</h2>
        ${token ? '<button class="btn btn-primary" onclick="location.hash=\'#/ask\'">Ask a question</button>' : ''}
      </div>
      <div id="questions-list" class="question-list"></div>
    `;

    document.getElementById('search-btn').onclick = doSearch;
    document.getElementById('search-input').onkeydown = e => { if (e.key === 'Enter') doSearch(); };

    try {
      const tags = await apiFetch('/tags?limit=20');
      const cloud = document.getElementById('tags-cloud');
      if (tags && tags.length) {
        cloud.innerHTML = tags.map(t =>
          `<span class="tag" onclick="searchByTag('${esc(t.tag)}')">${esc(t.tag)}<span class="tag-count">${t.count}</span></span>`
        ).join('');
      }
    } catch (_) {}

    try {
      const questions = await apiFetch('/questions?limit=20');
      renderQuestionList(document.getElementById('questions-list'), questions);
    } catch (_) {
      document.getElementById('questions-list').innerHTML = '<div class="empty-state"><h3>No questions yet</h3><p>Be the first to ask.</p></div>';
    }
  }

  async function doSearch() {
    const q = document.getElementById('search-input').value.trim();
    if (!q) return;
    try {
      const data = await apiFetch('/search', {
        method: 'POST',
        body: JSON.stringify({ query: q, limit: 20 })
      });
      const list = document.getElementById('questions-list');
      if (data.results && data.results.length) {
        renderQuestionList(list, data.results);
      } else {
        list.innerHTML = '<div class="empty-state"><h3>No results</h3></div>';
      }
    } catch (e) {
      toast(e.message, 'error');
    }
  }

  window.searchByTag = function (tag) {
    document.getElementById('search-input').value = tag;
    doSearch();
  };

  function renderQuestionList(container, questions) {
    if (!questions || !questions.length) {
      container.innerHTML = '<div class="empty-state"><h3>No questions yet</h3></div>';
      return;
    }
    container.innerHTML = questions.map(q => `
      <div class="question-card" onclick="location.hash='#/q/${esc(q.id)}'">
        <div class="question-title">${esc(q.body)}</div>
        <div class="question-meta">
          <span class="temp-badge temp-${q.temperature}">${q.temperature}</span>
          <span>Score: ${q.score}</span>
          <span>${q.child_count} replies</span>
          <span>${timeAgo(q.created_at)}</span>
        </div>
      </div>
    `).join('');
  }

  async function renderTree(app, nodeId) {
    app.innerHTML = '<div class="empty-state">Loading tree...</div>';
    try {
      const tree = await apiFetch('/tree/' + nodeId + '?depth=50');
      app.innerHTML = '<div id="tree-root"></div>';
      renderNodeTree(document.getElementById('tree-root'), tree, 0);
    } catch (e) {
      app.innerHTML = `<div class="empty-state"><h3>Error</h3><p>${esc(e.message)}</p></div>`;
    }
  }

  function renderNodeTree(container, node, depth) {
    const div = document.createElement('div');
    div.className = `tree-node depth-${depth} type-${node.node_type}`;

    const scoreClass = node.score > 0 ? 'positive' : node.score < 0 ? 'negative' : '';
    const modelBadge = node.model_id ? `<span style="color:var(--purple);font-size:11px">[${esc(node.model_id)}]</span>` : '';

    div.innerHTML = `
      <div class="node-header">
        <span class="node-type-badge ${node.node_type}">${node.node_type}</span>
        ${modelBadge}
        <span class="node-score ${scoreClass}">${node.score > 0 ? '+' : ''}${node.score}</span>
        <span>${timeAgo(node.created_at)}</span>
        <span class="temp-badge temp-${node.temperature}">${node.temperature}</span>
      </div>
      <div class="node-body">${esc(node.body)}</div>
      <div class="node-actions">
        ${token ? `
          <button class="vote-btn" data-id="${node.id}" data-val="1">&#9650; Upvote</button>
          <button class="vote-btn" data-id="${node.id}" data-val="-1">&#9660; Downvote</button>
          <button class="vote-btn" data-id="${node.id}" data-action="reply">Reply</button>
          <button class="vote-btn" data-id="${node.id}" data-action="thank">Thank</button>
        ` : ''}
      </div>
      <div class="node-children"></div>
    `;

    // Wire up actions
    div.querySelectorAll('.vote-btn[data-val]').forEach(btn => {
      btn.onclick = async () => {
        try {
          await apiFetch('/vote', {
            method: 'POST',
            body: JSON.stringify({ node_id: btn.dataset.id, value: parseInt(btn.dataset.val) })
          });
          toast('Vote recorded', 'success');
        } catch (e) { toast(e.message, 'error'); }
      };
    });

    div.querySelector('.vote-btn[data-action="reply"]')?.addEventListener('click', () => {
      showReplyBox(div.querySelector('.node-children'), node.id, node.root_id);
    });

    div.querySelector('.vote-btn[data-action="thank"]')?.addEventListener('click', async () => {
      try {
        await apiFetch('/thank', {
          method: 'POST',
          body: JSON.stringify({ node_id: node.id })
        });
        toast('Thanks sent!', 'success');
      } catch (e) { toast(e.message, 'error'); }
    });

    container.appendChild(div);

    if (node.children) {
      const childrenDiv = div.querySelector('.node-children');
      node.children.forEach(child => renderNodeTree(childrenDiv, child, depth + 1));
    }
  }

  function showReplyBox(container, parentId, rootId) {
    if (container.querySelector('.reply-box')) return;
    const box = document.createElement('div');
    box.className = 'reply-box';
    box.innerHTML = `
      <div class="form-group">
        <label>Type</label>
        <select class="form-select" id="reply-type">
          <option value="answer">Answer</option>
          <option value="evidence">Evidence</option>
          <option value="objection">Objection</option>
          <option value="precision">Precision</option>
          <option value="correction">Correction</option>
          <option value="synthesis">Synthesis</option>
        </select>
      </div>
      <div class="form-group">
        <textarea class="form-textarea" id="reply-body" placeholder="Write your contribution..."></textarea>
      </div>
      <div style="display:flex;gap:8px">
        <button class="btn btn-primary btn-sm" id="reply-submit">Submit</button>
        <button class="btn btn-sm" id="reply-cancel">Cancel</button>
      </div>
    `;

    box.querySelector('#reply-cancel').onclick = () => box.remove();
    box.querySelector('#reply-submit').onclick = async () => {
      const body = box.querySelector('#reply-body').value.trim();
      const nodeType = box.querySelector('#reply-type').value;
      if (!body) return toast('Body is required', 'error');

      try {
        await apiFetch('/answer', {
          method: 'POST',
          body: JSON.stringify({ parent_id: parentId, body, node_type: nodeType })
        });
        toast('Contribution added', 'success');
        // Refresh tree
        location.hash = '#/q/' + rootId;
        route();
      } catch (e) { toast(e.message, 'error'); }
    };

    container.prepend(box);
  }

  function renderLogin(app) {
    app.innerHTML = `
      <div style="max-width:400px;margin:48px auto">
        <h2 style="margin-bottom:24px">Login</h2>
        <div class="form-group">
          <label>Handle</label>
          <input class="form-input" id="login-handle" placeholder="your_handle">
        </div>
        <div class="form-group">
          <label>Password</label>
          <input class="form-input" id="login-password" type="password" placeholder="password">
        </div>
        <button class="btn btn-primary" id="login-btn" style="width:100%">Login</button>
        <p style="margin-top:16px;font-size:14px;color:var(--text-muted)">
          No account? <a href="#/register">Register</a>
        </p>
      </div>
    `;

    document.getElementById('login-btn').onclick = async () => {
      const handle = document.getElementById('login-handle').value.trim();
      const password = document.getElementById('login-password').value;
      try {
        const data = await apiFetch('/login', {
          method: 'POST',
          body: JSON.stringify({ handle, password })
        });
        token = data.token;
        localStorage.setItem('ht_token', token);
        updateHeader();
        location.hash = '#/';
      } catch (e) { toast(e.message, 'error'); }
    };
  }

  function renderRegister(app) {
    app.innerHTML = `
      <div style="max-width:400px;margin:48px auto">
        <h2 style="margin-bottom:24px">Register</h2>
        <div class="form-group">
          <label>Handle</label>
          <input class="form-input" id="reg-handle" placeholder="your_handle">
        </div>
        <div class="form-group">
          <label>Email (optional)</label>
          <input class="form-input" id="reg-email" type="email" placeholder="email@example.com">
        </div>
        <div class="form-group">
          <label>Password (min 8 characters)</label>
          <input class="form-input" id="reg-password" type="password" placeholder="password">
        </div>
        <button class="btn btn-primary" id="reg-btn" style="width:100%">Register</button>
        <p style="margin-top:16px;font-size:14px;color:var(--text-muted)">
          Already have an account? <a href="#/login">Login</a>
        </p>
      </div>
    `;

    document.getElementById('reg-btn').onclick = async () => {
      const handle = document.getElementById('reg-handle').value.trim();
      const email = document.getElementById('reg-email').value.trim();
      const password = document.getElementById('reg-password').value;
      try {
        const data = await apiFetch('/register', {
          method: 'POST',
          body: JSON.stringify({ handle, email, password })
        });
        token = data.token;
        localStorage.setItem('ht_token', token);
        updateHeader();
        location.hash = '#/';
        toast('Welcome to horostracker!', 'success');
      } catch (e) { toast(e.message, 'error'); }
    };
  }

  function renderAskForm(app) {
    if (!token) { location.hash = '#/login'; return; }

    app.innerHTML = `
      <div style="max-width:640px;margin:24px auto">
        <h2 style="margin-bottom:24px">Ask a question</h2>
        <div class="form-group">
          <label>Your question</label>
          <textarea class="form-textarea" id="ask-body" placeholder="What do you want to know?" style="min-height:140px"></textarea>
        </div>
        <div class="form-group">
          <label>Tags (comma-separated)</label>
          <input class="form-input" id="ask-tags" placeholder="golang, health, law...">
        </div>
        <button class="btn btn-primary" id="ask-btn">Submit question</button>
      </div>
    `;

    document.getElementById('ask-btn').onclick = async () => {
      const body = document.getElementById('ask-body').value.trim();
      const tagsRaw = document.getElementById('ask-tags').value.trim();
      const tags = tagsRaw ? tagsRaw.split(',').map(t => t.trim()).filter(Boolean) : [];

      if (!body) return toast('Question body is required', 'error');

      try {
        const data = await apiFetch('/ask', {
          method: 'POST',
          body: JSON.stringify({ body, tags })
        });
        toast('Question created!', 'success');
        location.hash = '#/q/' + data.node.id;
      } catch (e) { toast(e.message, 'error'); }
    };
  }

  async function renderProfile(app, handle) {
    app.innerHTML = '<div class="empty-state">Loading profile...</div>';
    try {
      const user = await apiFetch('/user/' + handle);
      let btsTagsHtml = '';
      try {
        const tags = JSON.parse(user.bountytreescore_tags || '{}');
        btsTagsHtml = Object.entries(tags).map(([k, v]) =>
          `<span class="tag">${esc(k)}: ${v}</span>`
        ).join('');
      } catch (_) {}

      app.innerHTML = `
        <div class="profile-header">
          <div class="profile-avatar">${esc(user.handle[0].toUpperCase())}</div>
          <div>
            <h2>${esc(user.handle)}</h2>
            <p style="color:var(--text-muted);font-size:14px">Joined ${timeAgo(user.created_at)}</p>
          </div>
        </div>
        <div class="profile-stats">
          <div class="stat-card">
            <div class="stat-value">${user.reputation}</div>
            <div class="stat-label">Reputation</div>
          </div>
          <div class="stat-card">
            <div class="stat-value">${user.bountytreescore_total}</div>
            <div class="stat-label">Bountytreescore</div>
          </div>
        </div>
        ${btsTagsHtml ? `<h3 style="margin-bottom:12px">Expertise by tag</h3><div class="tags-cloud">${btsTagsHtml}</div>` : ''}
      `;
    } catch (e) {
      app.innerHTML = `<div class="empty-state"><h3>User not found</h3></div>`;
    }
  }

  function renderBench(app) {
    app.innerHTML = `
      <div style="max-width:640px;margin:24px auto">
        <h2 style="margin-bottom:16px">LLM Benchmark</h2>
        <p style="color:var(--text-muted);margin-bottom:24px">
          Compare LLM performance across question domains.
          Benchmarks are generated from real community evaluations.
        </p>
        <div class="empty-state">
          <h3>Coming soon</h3>
          <p>LLM benchmark data will appear here once questions are answered by multiple models.</p>
        </div>
      </div>
    `;
  }

  // --- Utilities ---

  function esc(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  function timeAgo(dateStr) {
    if (!dateStr) return '';
    const date = new Date(dateStr);
    const now = new Date();
    const diff = (now - date) / 1000;
    if (diff < 60) return 'just now';
    if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
    if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
    if (diff < 2592000) return Math.floor(diff / 86400) + 'd ago';
    return date.toLocaleDateString();
  }
})();
