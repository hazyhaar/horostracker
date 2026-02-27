// horostracker — vanilla JS frontend
(function () {
  'use strict';

  const API = '/api';
  let token = localStorage.getItem('ht_token');
  let currentUser = null;
  let currentViewMode = 'ombilical';

  // --- data-nav delegation ---
  document.addEventListener('click', function (e) {
    const navEl = e.target.closest('[data-nav]');
    if (navEl) {
      e.preventDefault();
      location.hash = navEl.dataset.nav;
    }
  });

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
    if (hash === '/admin') return renderAdmin(app);
    if (hash === '/admin/workflows') return renderAdminWorkflows(app);
    if (hash === '/operator/workflows') return renderOperatorWorkflows(app);
    if (hash === '/provider/workflows') return renderProviderWorkflows(app);

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
        const isOp = user.role === 'operator';
        actions.innerHTML = `
          <span class="header-user">${esc(user.handle)} · ${user.reputation} rep</span>
          <button class="btn btn-sm" data-nav="#/u/${esc(user.handle)}">Profile</button>
          ${isOp ? '<button class="btn btn-sm" data-nav="#/admin">Admin</button>' : ''}
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
        <button class="btn btn-sm" data-nav="#/login">Login</button>
        <button class="btn btn-sm btn-primary" data-nav="#/register">Register</button>
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
        ${token ? '<button class="btn btn-primary" data-nav="#/ask">Ask a question</button>' : ''}
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
          `<span class="tag" data-tag="${esc(t.tag)}">${esc(t.tag)}<span class="tag-count">${t.count}</span></span>`
        ).join('');
        cloud.addEventListener('click', e => {
          const tag = e.target.closest('[data-tag]');
          if (tag) { searchByTag(tag.dataset.tag); }
        });
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

  function searchByTag(tag) {
    document.getElementById('search-input').value = tag;
    doSearch();
  }
  window.searchByTag = searchByTag;

  function renderQuestionList(container, questions) {
    if (!questions || !questions.length) {
      container.innerHTML = '<div class="empty-state"><h3>No questions yet</h3></div>';
      return;
    }
    container.innerHTML = questions.map(q => `
      <div class="question-card" data-nav="#/q/${esc(q.id)}">
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
      app.innerHTML = `
        <div class="view-mode-switcher">
          <button class="view-mode-btn${currentViewMode === 'ombilical' ? ' active' : ''}" data-mode="ombilical">Proof Tree</button>
          <button class="view-mode-btn${currentViewMode === 'tree' ? ' active' : ''}" data-mode="tree">Tree</button>
          <button class="view-mode-btn${currentViewMode === 'fishbone' ? ' active' : ''}" data-mode="fishbone">Fishbone</button>
        </div>
        <div id="tree-root"></div>
      `;
      app.querySelector('.view-mode-switcher').addEventListener('click', e => {
        const btn = e.target.closest('[data-mode]');
        if (!btn) return;
        currentViewMode = btn.dataset.mode;
        renderCurrentMode(document.getElementById('tree-root'), tree);
        app.querySelectorAll('.view-mode-btn').forEach(b => b.classList.toggle('active', b.dataset.mode === currentViewMode));
      });
      renderCurrentMode(document.getElementById('tree-root'), tree);
    } catch (e) {
      app.innerHTML = `<div class="empty-state"><h3>Error</h3><p>${esc(e.message)}</p></div>`;
    }
  }

  function renderCurrentMode(container, tree) {
    container.innerHTML = '';
    if (currentViewMode === 'ombilical') {
      renderOmbilical(container, tree, 0);
    } else if (currentViewMode === 'fishbone') {
      renderFishbone(container, tree);
    } else {
      renderNodeTree(container, tree, 0);
    }
  }

  // --- Collapsible Node Tree ---

  function extractNodeTitle(body) {
    if (!body) return '';
    const first = body.split('\n')[0];
    return first.length > 80 ? first.slice(0, 77) + '...' : first;
  }

  function countDescendants(node) {
    if (!node.children || !node.children.length) return 0;
    let count = node.children.length;
    node.children.forEach(c => { count += countDescendants(c); });
    return count;
  }

  // Check if a claim node has any ancestor or sibling piece node in the tree
  function isUnsourcedClaim(node) {
    if (node.node_type !== 'claim') return false;
    if (!node.children || !node.children.length) return true;
    return !node.children.some(c => c.node_type === 'piece');
  }

  function renderNodeTree(container, node, depth) {
    const div = document.createElement('div');
    div.className = `tree-node depth-${depth} type-${node.node_type} node-expanded`;
    div.dataset.id = node.id;

    const scoreClass = node.score > 0 ? 'positive' : node.score < 0 ? 'negative' : '';
    const modelBadge = node.model_id ? `<span style="color:var(--purple);font-size:11px">[${esc(node.model_id)}]</span>` : '';
    const handleBadge = node.author_handle ? `<span style="font-size:12px;color:var(--text-muted)">@${esc(node.author_handle)}</span>` : '';
    const descCount = countDescendants(node);
    const hasChildren = node.children && node.children.length > 0;
    const unsourcedBadge = isUnsourcedClaim(node) ? '<span class="unsourced-badge">non sourcé</span>' : '';

    div.innerHTML = `
      <div class="node-header-compact">
        ${hasChildren ? '<button class="node-expand-btn" title="Collapse">&#9660;</button>' : '<span style="width:20px;display:inline-block"></span>'}
        <span class="node-type-badge ${node.node_type}">${node.node_type}</span>
        ${unsourcedBadge}
        <span class="node-title-text">${esc(extractNodeTitle(node.body))}</span>
        ${descCount > 0 ? `<span class="node-child-count">${descCount}</span>` : ''}
        <span class="node-score ${scoreClass}">${node.score > 0 ? '+' : ''}${node.score}</span>
      </div>
      <div class="node-header">
        ${modelBadge}
        ${handleBadge}
        <span>${timeAgo(node.created_at)}</span>
        <span class="temp-badge temp-${node.temperature}">${node.temperature}</span>
      </div>
      <div class="node-body">${formatBody(node.body)}</div>
      <div class="node-actions">
        ${token ? `
          <button class="vote-btn" data-id="${node.id}" data-val="1">&#9650; Upvote</button>
          <button class="vote-btn" data-id="${node.id}" data-val="-1">&#9660; Downvote</button>
          <button class="vote-btn" data-id="${node.id}" data-action="reply">Reply</button>
          <button class="vote-btn" data-id="${node.id}" data-action="thank">Thank</button>
          ${(currentUser && (node.author_id === currentUser.id || currentUser.role === 'operator'))
            ? '<button class="vote-btn btn-danger" data-id="' + node.id + '" data-action="delete">Delete</button>'
            : ''}
          ${(node.node_type === 'claim' && currentUser && (node.author_id === currentUser.id || currentUser.role === 'operator'))
            ? '<button class="vote-btn" data-id="' + node.id + '" data-action="decompose" style="color:var(--green)">Decompose</button>'
            : ''}
          ${(node.node_type === 'piece' || node.node_type === 'claim')
            ? '<button class="vote-btn" data-id="' + node.id + '" data-action="add-source" style="color:var(--purple)">Add source</button>'
            : ''}
        ` : ''}
      </div>
      <div class="node-assertions" data-node-id="${node.id}"></div>
      <div class="node-sources" data-node-id="${node.id}"></div>
      <div class="node-children"></div>
    `;

    // Collapse/expand toggle
    const expandBtn = div.querySelector('.node-expand-btn');
    if (expandBtn) {
      expandBtn.addEventListener('click', () => {
        const isExpanded = div.classList.contains('node-expanded');
        if (isExpanded) {
          div.classList.remove('node-expanded');
          div.classList.add('node-collapsed');
          expandBtn.innerHTML = '&#9654;';
          expandBtn.classList.add('node-collapse-btn');
          expandBtn.title = 'Expand';
        } else {
          div.classList.add('node-expanded');
          div.classList.remove('node-collapsed');
          expandBtn.innerHTML = '&#9660;';
          expandBtn.classList.remove('node-collapse-btn');
          expandBtn.title = 'Collapse';
        }
      });
    }

    // Wire up vote actions
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

    // Delete handler
    div.querySelector('.vote-btn[data-action="delete"]')?.addEventListener('click', async () => {
      if (!confirm('Supprimer ce node ? Cette action est irréversible visuellement.')) return;
      try {
        await apiFetch('/node/' + node.id, { method: 'DELETE' });
        toast('Node supprimé', 'success');
        route();
      } catch (e) { toast(e.message, 'error'); }
    });

    // Decompose handler
    div.querySelector('.vote-btn[data-action="decompose"]')?.addEventListener('click', async () => {
      const panel = div.querySelector('.node-assertions');
      if (panel.querySelector('.decompose-panel')) return;
      panel.innerHTML = '<div class="decompose-panel"><p style="color:var(--text-muted)">Décomposition en cours...</p></div>';
      try {
        const data = await apiFetch('/node/' + node.id + '/decompose', { method: 'POST' });
        renderDecomposePanel(panel, node.id, data.assertions || []);
      } catch (e) {
        panel.innerHTML = '';
        toast(e.message, 'error');
      }
    });

    // Add source handler
    div.querySelector('.vote-btn[data-action="add-source"]')?.addEventListener('click', () => {
      const srcDiv = div.querySelector('.node-sources');
      if (srcDiv.querySelector('.source-form')) return;
      showSourceForm(srcDiv, node.id);
    });

    // Load existing sub-claims for root claim nodes
    if (node.node_type === 'claim' && depth === 0) {
      loadAssertions(div.querySelector('.node-assertions'), node.id);
    }

    // Load existing sources for all piece and claim nodes
    if (node.node_type === 'piece' || node.node_type === 'claim') {
      loadSources(div.querySelector('.node-sources'), node.id);
    }

    container.appendChild(div);

    if (node.children) {
      const childrenDiv = div.querySelector('.node-children');
      node.children.forEach(child => renderNodeTree(childrenDiv, child, depth + 1));
    }
  }

  function formatBody(body) {
    if (!body) return '';
    return esc(body);
  }

  // --- Fishbone Diagram ---

  function renderFishbone(container, tree) {
    const fb = document.createElement('div');
    fb.className = 'fishbone';

    // Head (question)
    const head = document.createElement('div');
    head.className = 'fb-head';
    head.innerHTML = `<h3>${esc(extractNodeTitle(tree.body))}</h3>`;
    fb.appendChild(head);

    // Spine
    const spine = document.createElement('div');
    spine.className = 'fb-spine';

    if (tree.children && tree.children.length) {
      for (let i = 0; i < tree.children.length; i += 2) {
        const pair = document.createElement('div');
        pair.className = 'fb-pair';

        // Top branch
        pair.appendChild(buildFishboneBranch(tree.children[i], 'top'));

        // Bottom branch
        if (tree.children[i + 1]) {
          pair.appendChild(buildFishboneBranch(tree.children[i + 1], 'bottom'));
        }

        spine.appendChild(pair);
      }
    }
    fb.appendChild(spine);
    container.appendChild(fb);
  }

  function buildFishboneBranch(node, position) {
    const branch = document.createElement('div');
    branch.className = `fb-branch fb-branch-${position}`;

    const connector = document.createElement('div');
    connector.className = 'fb-connector';
    const dot = document.createElement('div');
    dot.className = 'fb-dot';
    connector.appendChild(dot);
    branch.appendChild(connector);

    const card = document.createElement('div');
    card.className = `fb-card type-${node.node_type}`;
    card.innerHTML = `
      <span class="node-type-badge ${node.node_type}">${node.node_type}</span>
      <p>${esc(extractNodeTitle(node.body))}</p>
      <span style="font-size:11px;color:var(--text-muted)">${node.score > 0 ? '+' : ''}${node.score} · ${countDescendants(node)} children</span>
    `;
    card.addEventListener('click', () => { location.hash = '#/q/' + node.id; });
    branch.appendChild(card);

    return branch;
  }

  // --- Ombilical proof tree ---

  function classifyChildren(children) {
    const groups = {};
    for (const child of children) {
      const t = child.node_type || 'other';
      if (!groups[t]) groups[t] = [];
      groups[t].push(child);
    }
    return groups;
  }

  function renderOmbilical(container, node, depth) {
    const subtree = document.createElement('div');
    subtree.className = 'omb-subtree';
    subtree.dataset.id = node.id;

    // Parent card
    const parentCard = document.createElement('div');
    parentCard.className = 'omb-parent-card';
    const scoreClass = node.score > 0 ? 'positive' : node.score < 0 ? 'negative' : '';
    const modelBadge = node.model_id ? `<span style="color:var(--purple);font-size:11px">[${esc(node.model_id)}]</span>` : '';
    const handleBadge = node.author_handle ? `<span style="font-size:12px;color:var(--text-muted)">@${esc(node.author_handle)}</span>` : '';

    parentCard.innerHTML = `
      <div class="omb-parent-meta">
        <span class="node-type-badge ${node.node_type}">${node.node_type}</span>
        <span class="node-score ${scoreClass}">${node.score > 0 ? '+' : ''}${node.score}</span>
        <span class="temp-badge temp-${node.temperature}">${node.temperature}</span>
        ${modelBadge}
        ${handleBadge}
        <span>${timeAgo(node.created_at)}</span>
      </div>
      <div class="omb-parent-title">${esc(extractNodeTitle(node.body))}</div>
      <div class="omb-parent-body">${formatBody(node.body)}</div>
      <div class="omb-parent-actions">
        ${token ? `
          <button class="vote-btn" data-id="${node.id}" data-val="1">&#9650; Upvote</button>
          <button class="vote-btn" data-id="${node.id}" data-val="-1">&#9660; Downvote</button>
          <button class="vote-btn" data-id="${node.id}" data-action="reply">Reply</button>
          <button class="vote-btn" data-id="${node.id}" data-action="thank">Thank</button>
          ${(currentUser && (node.author_id === currentUser.id || currentUser.role === 'operator'))
            ? '<button class="vote-btn btn-danger" data-id="' + node.id + '" data-action="delete">Delete</button>'
            : ''}
          ${(node.node_type === 'claim' && currentUser && (node.author_id === currentUser.id || currentUser.role === 'operator'))
            ? '<button class="vote-btn" data-id="' + node.id + '" data-action="decompose" style="color:var(--green)">Decompose</button>'
            : ''}
          ${(node.node_type === 'piece' || node.node_type === 'claim')
            ? '<button class="vote-btn" data-id="' + node.id + '" data-action="add-source" style="color:var(--purple)">Add source</button>'
            : ''}
        ` : ''}
      </div>
      <div class="node-assertions" data-node-id="${node.id}"></div>
      <div class="node-sources" data-node-id="${node.id}"></div>
    `;

    // Toggle parent card expansion
    parentCard.addEventListener('click', (e) => {
      if (e.target.closest('.vote-btn') || e.target.closest('.reply-box') || e.target.closest('.source-form') || e.target.closest('.decompose-panel')) return;
      parentCard.classList.toggle('omb-parent-expanded');
    });

    // Wire parent card actions
    wireOmbilicalActions(parentCard, node);

    subtree.appendChild(parentCard);

    // Load sub-claims/sources for parent
    if (node.node_type === 'claim' && depth === 0) {
      loadAssertions(parentCard.querySelector('.node-assertions'), node.id);
    }
    if (node.node_type === 'piece' || node.node_type === 'claim') {
      loadSources(parentCard.querySelector('.node-sources'), node.id);
    }

    // Children classified by type into columns
    if (node.children && node.children.length > 0) {
      const columns = classifyChildren(node.children);
      const columnsDiv = document.createElement('div');
      columnsDiv.className = 'omb-columns';

      for (const [type, kids] of Object.entries(columns)) {
        const col = document.createElement('div');
        col.className = 'omb-column';
        const header = document.createElement('div');
        header.className = 'omb-column-header';
        header.textContent = type + ' (' + kids.length + ')';
        col.appendChild(header);

        const sorted = [...kids].sort((a, b) => b.score - a.score);
        for (const child of sorted) {
          col.appendChild(buildOmbilicalBubble(child, depth));
        }
        columnsDiv.appendChild(col);
      }

      subtree.appendChild(columnsDiv);
    }

    container.appendChild(subtree);
  }

  function buildOmbilicalBubble(node, parentDepth) {
    const bubble = document.createElement('div');
    bubble.className = 'omb-bubble omb-collapsed';
    bubble.dataset.id = node.id;

    const scoreClass = node.score > 0 ? 'positive' : node.score < 0 ? 'negative' : '';
    const descCount = countDescendants(node);

    bubble.innerHTML = `
      <div class="omb-bubble-card">
        <span class="node-type-badge ${node.node_type}">${node.node_type}</span>
        <span class="omb-bubble-title">${esc(extractNodeTitle(node.body))}</span>
        ${descCount > 0 ? `<span class="node-child-count">${descCount}</span>` : ''}
        <span class="omb-bubble-score ${scoreClass}">${node.score > 0 ? '+' : ''}${node.score}</span>
      </div>
      <div class="omb-bubble-expanded"></div>
    `;

    bubble.querySelector('.omb-bubble-card').addEventListener('click', () => {
      expandBubble(bubble, node, parentDepth + 1);
    });

    return bubble;
  }

  function expandBubble(bubbleEl, node, depth) {
    const wasExpanded = bubbleEl.classList.contains('omb-expanded');

    if (wasExpanded) {
      bubbleEl.classList.remove('omb-expanded');
      bubbleEl.classList.add('omb-collapsed');
      return;
    }

    bubbleEl.classList.remove('omb-collapsed');
    bubbleEl.classList.add('omb-expanded');

    if (bubbleEl.dataset.rendered) return;

    const expandedDiv = bubbleEl.querySelector('.omb-bubble-expanded');
    const modelBadge = node.model_id ? `<span style="color:var(--purple);font-size:11px">[${esc(node.model_id)}]</span>` : '';
    const handleBadge = node.author_handle ? `<span style="font-size:12px;color:var(--text-muted)">@${esc(node.author_handle)}</span>` : '';

    expandedDiv.innerHTML = `
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:8px;font-size:13px;color:var(--text-muted)">
        ${modelBadge} ${handleBadge}
        <span>${timeAgo(node.created_at)}</span>
        <span class="temp-badge temp-${node.temperature}">${node.temperature}</span>
      </div>
      <div class="omb-expanded-body">${formatBody(node.body)}</div>
      <div class="omb-expanded-actions">
        ${token ? `
          <button class="vote-btn" data-id="${node.id}" data-val="1">&#9650; Upvote</button>
          <button class="vote-btn" data-id="${node.id}" data-val="-1">&#9660; Downvote</button>
          <button class="vote-btn" data-id="${node.id}" data-action="reply">Reply</button>
          <button class="vote-btn" data-id="${node.id}" data-action="thank">Thank</button>
          ${(currentUser && (node.author_id === currentUser.id || currentUser.role === 'operator'))
            ? '<button class="vote-btn btn-danger" data-id="' + node.id + '" data-action="delete">Delete</button>'
            : ''}
          ${(node.node_type === 'claim' && currentUser && (node.author_id === currentUser.id || currentUser.role === 'operator'))
            ? '<button class="vote-btn" data-id="' + node.id + '" data-action="decompose" style="color:var(--green)">Decompose</button>'
            : ''}
          ${(node.node_type === 'piece' || node.node_type === 'claim')
            ? '<button class="vote-btn" data-id="' + node.id + '" data-action="add-source" style="color:var(--purple)">Add source</button>'
            : ''}
        ` : ''}
      </div>
      <div class="omb-expanded-assertions" data-node-id="${node.id}"></div>
      <div class="omb-expanded-sources" data-node-id="${node.id}"></div>
      <div class="omb-expanded-subtree"></div>
    `;

    // Wire actions
    wireOmbilicalActions(expandedDiv, node);

    // Load sub-claims for claim nodes
    if (node.node_type === 'claim') {
      loadAssertions(expandedDiv.querySelector('.omb-expanded-assertions'), node.id);
    }

    // Load sources for all node types
    if (node.node_type === 'piece' || node.node_type === 'claim') {
      loadSources(expandedDiv.querySelector('.omb-expanded-sources'), node.id);
    }

    // Render subtree if children exist
    if (node.children && node.children.length > 0) {
      renderOmbilical(expandedDiv.querySelector('.omb-expanded-subtree'), node, depth);
    }

    bubbleEl.dataset.rendered = 'true';
  }

  function wireOmbilicalActions(el, node) {
    el.querySelectorAll('.vote-btn[data-val]').forEach(btn => {
      btn.onclick = async (e) => {
        e.stopPropagation();
        try {
          await apiFetch('/vote', {
            method: 'POST',
            body: JSON.stringify({ node_id: btn.dataset.id, value: parseInt(btn.dataset.val) })
          });
          toast('Vote recorded', 'success');
        } catch (e) { toast(e.message, 'error'); }
      };
    });

    const replyBtn = el.querySelector('.vote-btn[data-action="reply"]');
    if (replyBtn) {
      replyBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        const target = el.querySelector('.omb-expanded-subtree') || el.querySelector('.node-assertions') || el;
        showReplyBox(target, node.id, node.root_id);
      });
    }

    const thankBtn = el.querySelector('.vote-btn[data-action="thank"]');
    if (thankBtn) {
      thankBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        try {
          await apiFetch('/thank', { method: 'POST', body: JSON.stringify({ node_id: node.id }) });
          toast('Thanks sent!', 'success');
        } catch (e) { toast(e.message, 'error'); }
      });
    }

    const deleteBtn = el.querySelector('.vote-btn[data-action="delete"]');
    if (deleteBtn) {
      deleteBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        if (!confirm('Supprimer ce node ? Cette action est irréversible visuellement.')) return;
        try {
          await apiFetch('/node/' + node.id, { method: 'DELETE' });
          toast('Node supprimé', 'success');
          route();
        } catch (e) { toast(e.message, 'error'); }
      });
    }

    const decomposeBtn = el.querySelector('.vote-btn[data-action="decompose"]');
    if (decomposeBtn) {
      decomposeBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const panel = el.querySelector('.node-assertions') || el.querySelector('.omb-expanded-assertions');
        if (!panel || panel.querySelector('.decompose-panel')) return;
        panel.innerHTML = '<div class="decompose-panel"><p style="color:var(--text-muted)">Décomposition en cours...</p></div>';
        try {
          const data = await apiFetch('/node/' + node.id + '/decompose', { method: 'POST' });
          renderDecomposePanel(panel, node.id, data.assertions || []);
        } catch (e) {
          panel.innerHTML = '';
          toast(e.message, 'error');
        }
      });
    }

    const sourceBtn = el.querySelector('.vote-btn[data-action="add-source"]');
    if (sourceBtn) {
      sourceBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        const srcDiv = el.querySelector('.node-sources') || el.querySelector('.omb-expanded-sources');
        if (!srcDiv || srcDiv.querySelector('.source-form')) return;
        showSourceForm(srcDiv, node.id);
      });
    }
  }

  // --- Reply box ---

  function showReplyBox(container, parentId, rootId) {
    if (container.querySelector('.reply-box')) return;
    const box = document.createElement('div');
    box.className = 'reply-box';
    box.innerHTML = `
      <div class="form-group">
        <label>Type</label>
        <select class="form-select" id="reply-type">
          <option value="claim">Claim</option>
          <option value="piece">Piece</option>
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
        location.hash = '#/q/' + rootId;
        route();
      } catch (e) { toast(e.message, 'error'); }
    };

    container.prepend(box);
  }

  // --- Auth pages ---

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
        toast('Claim created!', 'success');

        // If providers returned decompositions, show benchmark panel
        if (data.decompositions && data.decompositions.length > 0) {
          const wrap = document.createElement('div');
          wrap.style.cssText = 'max-width:900px;margin:24px auto';
          app.innerHTML = '';
          app.appendChild(wrap);
          renderDecomposeBenchmark(wrap, data.node.id, data.decompositions);
        } else {
          location.hash = '#/q/' + data.node.id;
        }
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

  // --- Admin Page ---

  async function renderAdmin(app) {
    if (!token) { location.hash = '#/login'; return; }

    app.innerHTML = `
      <div style="max-width:960px;margin:24px auto">
        <h2 style="margin-bottom:16px">Admin Dashboard</h2>
        <div style="display:flex;gap:12px;margin-bottom:24px">
          <button class="btn btn-primary admin-bootstrap-btn" id="admin-bootstrap-btn">Refresh Questions</button>
        </div>
        <div class="admin-questions" id="admin-questions"></div>
        <div class="admin-tree-wrap" id="admin-tree-wrap" style="margin-top:24px"></div>
      </div>
    `;

    document.getElementById('admin-bootstrap-btn').addEventListener('click', loadAdminQuestions);
    await loadAdminQuestions();
  }

  async function loadAdminQuestions() {
    const container = document.getElementById('admin-questions');
    if (!container) return;
    try {
      const questions = await apiFetch('/questions?limit=50');
      if (!questions || !questions.length) {
        container.innerHTML = '<div class="empty-state"><h3>No questions</h3></div>';
        return;
      }
      container.innerHTML = questions.map(q => `
        <div class="question-card admin-question-card" data-qid="${q.id}">
          <div class="question-title">${esc(q.body)}</div>
          <div class="question-meta">
            <span class="temp-badge temp-${q.temperature}">${q.temperature}</span>
            <span>Score: ${q.score}</span>
            <span>${q.child_count} replies</span>
          </div>
        </div>
      `).join('');
      container.addEventListener('click', e => {
        const card = e.target.closest('[data-qid]');
        if (card) loadAdminTree(card.dataset.qid);
      });
    } catch (e) {
      container.innerHTML = `<div class="empty-state"><h3>Error: ${esc(e.message)}</h3></div>`;
    }
  }

  async function loadAdminTree(questionId) {
    const wrap = document.getElementById('admin-tree-wrap');
    if (!wrap) return;
    wrap.innerHTML = '<div class="empty-state">Loading tree...</div>';
    try {
      const tree = await apiFetch('/tree/' + questionId + '?depth=50');
      wrap.innerHTML = `
        <h3>Tree: ${esc(extractNodeTitle(tree.body))}</h3>
        <div class="view-mode-switcher" style="margin:12px 0">
          <button class="view-mode-btn active" data-mode="tree">Tree</button>
          <button class="view-mode-btn" data-mode="fishbone">Fishbone</button>
        </div>
        <div id="admin-tree" class="admin-tree"></div>
        <div id="resolutions-wrap" class="resolutions-wrap" style="margin-top:24px"></div>
      `;
      const adminTree = document.getElementById('admin-tree');
      renderAdminNodeTree(adminTree, tree, 0);
      wrap.scrollIntoView({ behavior: 'smooth' });

      // View mode switching within admin
      wrap.querySelector('.view-mode-switcher').addEventListener('click', e => {
        const btn = e.target.closest('[data-mode]');
        if (!btn) return;
        wrap.querySelectorAll('.view-mode-btn').forEach(b => b.classList.toggle('active', b === btn));
        adminTree.innerHTML = '';
        if (btn.dataset.mode === 'fishbone') {
          renderFishbone(adminTree, tree);
        } else {
          renderAdminNodeTree(adminTree, tree, 0);
        }
      });

      // Load resolutions
      loadNodeResolutions(questionId);
      checkResolutionStatus(questionId);
    } catch (e) {
      wrap.innerHTML = `<div class="empty-state"><h3>Error: ${esc(e.message)}</h3></div>`;
    }
  }

  function renderAdminNodeTree(container, node, depth) {
    renderNodeTree(container, node, depth);
  }

  async function loadNodeResolutions(nodeId) {
    const wrap = document.getElementById('resolutions-wrap');
    if (!wrap) return;
    try {
      const data = await apiFetch('/resolution/' + nodeId + '/models');
      if (!data.models || !data.models.length) {
        wrap.innerHTML = '<p style="color:var(--text-muted)">No resolutions available.</p>';
        return;
      }
      wrap.innerHTML = '<h4>Resolutions</h4>' + data.models.map(model => `
        <div class="resolution-card" data-model="${esc(model)}">
          <div class="res-provider">${esc(model)}</div>
          <div class="res-compact">Loading...</div>
          <div class="res-expanded" style="display:none"></div>
        </div>
      `).join('');
      // Load each resolution
      for (const model of data.models) {
        loadResolutionContent(nodeId, model);
      }
    } catch (_) {
      wrap.innerHTML = '<p style="color:var(--text-muted)">No resolutions.</p>';
    }
  }

  async function loadResolutionContent(nodeId, model) {
    try {
      const data = await apiFetch('/resolution/' + nodeId + '/' + encodeURIComponent(model));
      const card = document.querySelector(`.resolution-card[data-model="${model}"]`);
      if (!card) return;
      const title = extractResolutionTitle(data.content || '');
      card.querySelector('.res-compact').textContent = title;
      card.querySelector('.res-expanded').innerHTML = formatBody(data.content || '');
      card.addEventListener('click', () => {
        const exp = card.querySelector('.res-expanded');
        exp.style.display = exp.style.display === 'none' ? 'block' : 'none';
      });
    } catch (_) {}
  }

  function extractResolutionTitle(content) {
    if (!content) return 'Resolution';
    const firstLine = content.split('\n')[0];
    return firstLine.length > 100 ? firstLine.slice(0, 97) + '...' : firstLine;
  }

  async function checkResolutionStatus(nodeId) {
    try {
      await apiFetch('/resolution/' + nodeId + '/models');
    } catch (_) {}
  }

  // --- Decompose Panel ---

  function renderDecomposePanel(container, questionId, assertions) {
    const panel = document.createElement('div');
    panel.className = 'decompose-panel';
    panel.innerHTML = `
      <h4>Assertions proposées</h4>
      <div class="assertion-list">
        ${assertions.map((a, i) => `
          <div class="assertion-item" data-idx="${i}">
            <input type="checkbox" checked>
            <span class="assertion-label">${esc(a)}</span>
            <button class="btn btn-sm assertion-edit-btn">Modifier</button>
            <textarea class="assertion-edit" style="display:none">${esc(a)}</textarea>
          </div>
        `).join('')}
      </div>
      <div style="display:flex;gap:8px;margin-top:12px">
        <button class="btn btn-sm" id="add-assertion-btn">+ Ajouter</button>
        <button class="btn btn-primary btn-sm" id="validate-assertions-btn">Valider</button>
        <button class="btn btn-sm" id="cancel-assertions-btn">Annuler</button>
      </div>
    `;

    // Wire edit buttons
    panel.querySelectorAll('.assertion-edit-btn').forEach(btn => {
      btn.onclick = () => {
        const item = btn.closest('.assertion-item');
        const label = item.querySelector('.assertion-label');
        const ta = item.querySelector('.assertion-edit');
        if (ta.style.display === 'none') {
          ta.style.display = '';
          ta.value = label.textContent;
          label.style.display = 'none';
          btn.textContent = 'OK';
        } else {
          label.textContent = ta.value.trim() || label.textContent;
          label.style.display = '';
          ta.style.display = 'none';
          btn.textContent = 'Modifier';
        }
      };
    });

    panel.querySelector('#add-assertion-btn').onclick = () => {
      const list = panel.querySelector('.assertion-list');
      const item = document.createElement('div');
      item.className = 'assertion-item';
      item.innerHTML = `<input type="checkbox" checked><span class="assertion-label" style="display:none"></span><button class="btn btn-sm assertion-edit-btn" style="display:none">Modifier</button><textarea class="assertion-edit" placeholder="Nouvelle assertion..."></textarea>`;
      list.appendChild(item);
    };

    panel.querySelector('#cancel-assertions-btn').onclick = () => {
      container.innerHTML = '';
    };

    panel.querySelector('#validate-assertions-btn').onclick = async () => {
      const items = panel.querySelectorAll('.assertion-item');
      const selected = [];
      items.forEach(item => {
        const cb = item.querySelector('input[type="checkbox"]');
        const ta = item.querySelector('.assertion-edit');
        const label = item.querySelector('.assertion-label');
        // Use textarea value if visible (editing), otherwise label text
        const text = (ta && ta.style.display !== 'none') ? ta.value.trim() : (label ? label.textContent.trim() : '');
        if (cb.checked && text) {
          selected.push(text);
        }
      });
      if (!selected.length) return toast('Sélectionnez au moins une assertion', 'error');
      try {
        const data = await apiFetch('/node/' + questionId + '/assertions', {
          method: 'POST',
          body: JSON.stringify({ assertions: selected })
        });
        toast(data.count + ' assertion(s) créée(s)', 'success');
        container.innerHTML = '';
        loadAssertions(container, questionId);
      } catch (e) { toast(e.message, 'error'); }
    };

    container.innerHTML = '';
    container.appendChild(panel);
  }

  async function loadAssertions(container, questionId) {
    try {
      const data = await apiFetch('/node/' + questionId + '/assertions');
      if (!data.assertions || !data.assertions.length) return;
      let html = '<div style="margin-top:8px"><span style="font-size:12px;color:var(--text-muted)">Sub-claims :</span> ';
      html += data.assertions.map(a =>
        `<a class="assertion-link" data-nav="#/q/${esc(a.id)}" title="${esc(a.body)}">${esc(a.body.length > 50 ? a.body.slice(0, 47) + '...' : a.body)}</a>`
      ).join('');
      html += '</div>';
      container.innerHTML += html;
    } catch (_) {}
  }

  // --- Source Form ---

  function showSourceForm(container, nodeId) {
    const form = document.createElement('div');
    form.className = 'source-form';
    form.innerHTML = `
      <select id="source-type-select">
        <option value="text">Texte anonymisé</option>
        <option value="url">URL</option>
      </select>
      <textarea id="source-content" placeholder="Collez le texte source anonymisé..."></textarea>
      <input id="source-url" placeholder="https://..." style="display:none">
      <input id="source-title" placeholder="Titre (optionnel)">
      <div style="display:flex;gap:8px">
        <button class="btn btn-primary btn-sm" id="source-submit-btn">Verser</button>
        <button class="btn btn-sm" id="source-cancel-btn">Annuler</button>
      </div>
    `;

    form.querySelector('#source-type-select').onchange = (e) => {
      const isText = e.target.value === 'text';
      form.querySelector('#source-content').style.display = isText ? '' : 'none';
      form.querySelector('#source-url').style.display = isText ? 'none' : '';
    };

    form.querySelector('#source-cancel-btn').onclick = () => form.remove();

    form.querySelector('#source-submit-btn').onclick = async () => {
      const type = form.querySelector('#source-type-select').value;
      const title = form.querySelector('#source-title').value.trim() || null;
      let payload = { title };

      if (type === 'text') {
        const text = form.querySelector('#source-content').value.trim();
        if (!text) return toast('Le texte source est requis', 'error');
        payload.content_text = text;
      } else {
        const url = form.querySelector('#source-url').value.trim();
        if (!url) return toast("L'URL est requise", 'error');
        payload.url = url;
      }

      try {
        await apiFetch('/node/' + nodeId + '/source', {
          method: 'POST',
          body: JSON.stringify(payload)
        });
        toast('Source versée', 'success');
        form.remove();
        loadSources(container, nodeId);
      } catch (e) { toast(e.message, 'error'); }
    };

    container.prepend(form);
  }

  async function loadSources(container, nodeId) {
    try {
      const data = await apiFetch('/node/' + nodeId + '/sources');
      if (!data.sources || !data.sources.length) return;

      // Remove existing source cards to avoid duplicates on refresh
      container.querySelectorAll('.source-card').forEach(c => c.remove());

      for (const src of data.sources) {
        const card = document.createElement('div');
        card.className = 'source-card';
        let inner = '';
        if (src.title) inner += `<div class="source-title">${esc(src.title)}</div>`;
        if (src.url) inner += `<div class="source-url">${esc(src.url)}</div>`;
        if (src.content_text) inner += `<div class="source-text">${esc(src.content_text.length > 200 ? src.content_text.slice(0, 197) + '...' : src.content_text)}</div>`;
        inner += `<div class="w5h1-badges" data-source-id="${src.id}"></div>`;
        card.innerHTML = inner;
        container.appendChild(card);

        // Load 5W1H badges
        load5W1H(card.querySelector('.w5h1-badges'), src.id);
      }
    } catch (_) {}
  }

  async function load5W1H(container, sourceId) {
    try {
      const data = await apiFetch('/source/' + sourceId + '/5w1h');
      if (!data.dimensions) return;
      const dimLabels = { who: 'Qui', what: 'Quoi', when: 'Quand', where: 'Où', why: 'Pourquoi', how: 'Comment' };
      let html = '';
      for (const [dim, entries] of Object.entries(data.dimensions)) {
        if (!entries || !entries.length) continue;
        html += `<span class="w5h1-badge ${dim}"><span class="w5h1-dim">${dimLabels[dim] || dim}:</span> ${esc(entries.join(', '))}</span>`;
      }
      if (html) container.innerHTML = html;
    } catch (_) {}
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

  // ===== VACF Workflows =====

  const STEP_TYPES = ['llm', 'sql', 'http', 'check'];
  const STEP_TYPE_COLORS = { llm: 'var(--accent)', sql: 'var(--green)', http: 'var(--orange)', check: 'var(--purple)' };
  const WF_TYPES = ['decompose','critique','source','factcheck','analyse','synthese','reformulation','media_export','contradiction_detection','completude','traduction','classification_epistemique','workflow_validation','model_discovery'];

  function workflowNav(role) {
    return `<div class="wf-nav" style="display:flex;gap:8px;margin-bottom:16px">
      <button class="btn btn-sm" data-nav="#/admin/workflows">Admin</button>
      <button class="btn btn-sm" data-nav="#/operator/workflows">Operator</button>
      <button class="btn btn-sm" data-nav="#/provider/workflows">Provider</button>
    </div>`;
  }

  async function renderAdminWorkflows(app) {
    if (!token) { location.hash = '#/login'; return; }
    app.innerHTML = '<div style="max-width:1200px;margin:24px auto">' + workflowNav('admin') +
      '<h2>Admin Workflows</h2>' +
      '<div style="display:flex;gap:8px;margin:16px 0">' +
        '<button class="btn btn-primary" id="wf-create-btn">New Workflow</button>' +
        '<button class="btn" id="wf-discover-btn">Discover Models</button>' +
      '</div>' +
      '<div id="wf-list"></div>' +
      '<div id="wf-models" style="margin-top:24px"></div>' +
      '<div id="wf-grants" style="margin-top:24px"></div>' +
    '</div>';

    document.getElementById('wf-create-btn').onclick = () => showWorkflowEditor(null, 'admin');
    document.getElementById('wf-discover-btn').onclick = async () => {
      try { await apiFetch('/models/discover', { method: 'POST' }); toast('Discovery started'); } catch(e) { toast(e.message, 'error'); }
    };
    await loadWorkflowList('admin');
    await loadModelsList();
    await loadGrantsList();
  }

  async function renderOperatorWorkflows(app) {
    if (!token) { location.hash = '#/login'; return; }
    app.innerHTML = '<div style="max-width:1200px;margin:24px auto">' + workflowNav('operator') +
      '<h2>Operator Workflows</h2>' +
      '<div style="margin:16px 0"><button class="btn btn-primary" id="wf-create-btn">New Workflow</button></div>' +
      '<div id="wf-list"></div>' +
    '</div>';
    document.getElementById('wf-create-btn').onclick = () => showWorkflowEditor(null, 'operator');
    await loadWorkflowList('operator');
  }

  async function renderProviderWorkflows(app) {
    if (!token) { location.hash = '#/login'; return; }
    app.innerHTML = '<div style="max-width:1200px;margin:24px auto">' + workflowNav('provider') +
      '<h2>Provider Workflows</h2>' +
      '<div style="display:flex;gap:8px;margin:16px 0;border-bottom:1px solid var(--border);padding-bottom:8px">' +
        '<button class="btn btn-sm provider-tab active" data-tab="workflows">Workflows</button>' +
        '<button class="btn btn-sm provider-tab" data-tab="models">My Models</button>' +
        '<button class="btn btn-sm provider-tab" data-tab="groups">Operator Groups</button>' +
        '<button class="btn btn-sm provider-tab" data-tab="rights">Model Rights</button>' +
      '</div>' +
      '<div id="provider-tab-content">' +
        '<div style="margin:16px 0"><button class="btn btn-primary" id="wf-create-btn">New Workflow</button></div>' +
        '<div id="wf-list"></div>' +
      '</div>' +
    '</div>';

    // Tab switching
    app.querySelectorAll('.provider-tab').forEach(btn => {
      btn.onclick = () => {
        app.querySelectorAll('.provider-tab').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        switchProviderTab(btn.dataset.tab);
      };
    });

    document.getElementById('wf-create-btn').onclick = () => showWorkflowEditor(null, 'provider');
    await loadWorkflowList('provider');
  }

  async function switchProviderTab(tab) {
    const content = document.getElementById('provider-tab-content');
    if (!content) return;

    if (tab === 'workflows') {
      content.innerHTML = '<div style="margin:16px 0"><button class="btn btn-primary" id="wf-create-btn">New Workflow</button></div><div id="wf-list"></div>';
      document.getElementById('wf-create-btn').onclick = () => showWorkflowEditor(null, 'provider');
      await loadWorkflowList('provider');
    } else if (tab === 'models') {
      await renderProviderModels(content);
    } else if (tab === 'groups') {
      await renderProviderGroups(content);
    } else if (tab === 'rights') {
      await renderProviderRights(content);
    }
  }

  // --- Provider Models Tab ---
  async function renderProviderModels(container) {
    container.innerHTML = '<h3>My Models</h3>' +
      '<div style="margin-bottom:12px;padding:12px;border:1px solid var(--border);border-radius:4px">' +
        '<div style="display:flex;gap:8px;flex-wrap:wrap;align-items:end">' +
          '<div><label style="display:block;font-size:12px">Model ID</label><input id="pm-model-id" class="search-input" style="width:200px" placeholder="provider/model-name"></div>' +
          '<div><label style="display:block;font-size:12px">Provider</label><input id="pm-provider" class="search-input" style="width:120px" placeholder="anthropic"></div>' +
          '<div><label style="display:block;font-size:12px">Model Name</label><input id="pm-model-name" class="search-input" style="width:180px" placeholder="claude-opus-4-6"></div>' +
          '<div><label style="display:block;font-size:12px">Display Name</label><input id="pm-display-name" class="search-input" style="width:180px" placeholder="Claude Opus"></div>' +
          '<button class="btn btn-primary btn-sm" id="pm-add-btn">Register Model</button>' +
        '</div>' +
      '</div>' +
      '<div id="pm-list"></div>';

    document.getElementById('pm-add-btn').onclick = async () => {
      try {
        await apiFetch('/models', {
          method: 'POST',
          body: JSON.stringify({
            model_id: document.getElementById('pm-model-id').value,
            provider: document.getElementById('pm-provider').value,
            model_name: document.getElementById('pm-model-name').value,
            display_name: document.getElementById('pm-display-name').value || null,
          })
        });
        toast('Model registered');
        await renderProviderModels(container);
      } catch(e) { toast(e.message, 'error'); }
    };

    try {
      const models = await apiFetch('/models');
      const myModels = models.filter(m => m.owner_id);
      const listEl = document.getElementById('pm-list');
      if (!myModels.length) {
        listEl.innerHTML = '<p style="color:var(--text-muted)">No provider-registered models yet.</p>';
      } else {
        listEl.innerHTML = '<table class="vacf-table"><thead><tr><th>Model ID</th><th>Provider</th><th>Name</th><th>Available</th></tr></thead><tbody>' +
          myModels.map(m => `<tr><td>${esc(m.model_id)}</td><td>${esc(m.provider)}</td><td>${esc(m.display_name || m.model_name)}</td><td>${m.is_available ? 'Yes' : 'No'}</td></tr>`).join('') +
          '</tbody></table>';
      }
    } catch(_) {}
  }

  // --- Provider Groups Tab ---
  async function renderProviderGroups(container) {
    container.innerHTML = '<h3>Operator Groups</h3>' +
      '<div style="margin-bottom:12px;padding:12px;border:1px solid var(--border);border-radius:4px">' +
        '<div style="display:flex;gap:8px;align-items:end">' +
          '<div><label style="display:block;font-size:12px">Group Name</label><input id="pg-name" class="search-input" style="width:200px"></div>' +
          '<div><label style="display:block;font-size:12px">Description</label><input id="pg-desc" class="search-input" style="width:300px"></div>' +
          '<button class="btn btn-primary btn-sm" id="pg-add-btn">Create Group</button>' +
        '</div>' +
      '</div>' +
      '<div id="pg-list"></div>';

    document.getElementById('pg-add-btn').onclick = async () => {
      try {
        await apiFetch('/operator-groups', {
          method: 'POST',
          body: JSON.stringify({
            name: document.getElementById('pg-name').value,
            description: document.getElementById('pg-desc').value,
          })
        });
        toast('Group created');
        await renderProviderGroups(container);
      } catch(e) { toast(e.message, 'error'); }
    };

    try {
      const groups = await apiFetch('/operator-groups');
      const listEl = document.getElementById('pg-list');
      if (!groups.length) {
        listEl.innerHTML = '<p style="color:var(--text-muted)">No groups yet. Create one to manage operators in bulk.</p>';
        return;
      }
      let html = '';
      for (const g of groups) {
        let members = [];
        try { members = await apiFetch('/operator-groups/' + g.group_id + '/members'); } catch(_) {}
        html += `<div style="border:1px solid var(--border);border-radius:4px;padding:12px;margin-bottom:12px">
          <div style="display:flex;justify-content:space-between;align-items:center">
            <div><strong>${esc(g.name)}</strong> <span style="color:var(--text-muted);font-size:12px">${esc(g.description || '')}</span></div>
            <div style="display:flex;gap:4px">
              <button class="btn btn-sm pg-del-btn" data-gid="${esc(g.group_id)}">Delete</button>
            </div>
          </div>
          <div style="margin-top:8px">
            <span style="font-size:12px;color:var(--text-muted)">Members (${members.length}):</span>
            ${members.map(m => `<span style="display:inline-flex;align-items:center;gap:4px;padding:2px 8px;margin:2px;background:var(--surface);border-radius:4px;font-size:12px">${esc(m.operator_id)} <button class="btn btn-sm pg-rm-member" data-gid="${esc(g.group_id)}" data-oid="${esc(m.operator_id)}" style="padding:0 4px;font-size:10px">x</button></span>`).join('')}
            <div style="margin-top:4px;display:flex;gap:4px;align-items:center">
              <input class="search-input pg-add-member-input" data-gid="${esc(g.group_id)}" placeholder="operator ID" style="width:200px;font-size:12px">
              <button class="btn btn-sm pg-add-member-btn" data-gid="${esc(g.group_id)}">Add</button>
            </div>
          </div>
        </div>`;
      }
      listEl.innerHTML = html;

      // Wire delete group
      listEl.querySelectorAll('.pg-del-btn').forEach(btn => {
        btn.onclick = async () => {
          if (!confirm('Delete this group?')) return;
          try {
            await apiFetch('/operator-groups/' + btn.dataset.gid, { method: 'DELETE' });
            toast('Group deleted');
            await renderProviderGroups(container);
          } catch(e) { toast(e.message, 'error'); }
        };
      });
      // Wire remove member
      listEl.querySelectorAll('.pg-rm-member').forEach(btn => {
        btn.onclick = async (e) => {
          e.stopPropagation();
          try {
            await apiFetch('/operator-groups/' + btn.dataset.gid + '/members/' + btn.dataset.oid, { method: 'DELETE' });
            toast('Member removed');
            await renderProviderGroups(container);
          } catch(e2) { toast(e2.message, 'error'); }
        };
      });
      // Wire add member
      listEl.querySelectorAll('.pg-add-member-btn').forEach(btn => {
        btn.onclick = async () => {
          const input = listEl.querySelector(`.pg-add-member-input[data-gid="${btn.dataset.gid}"]`);
          const opId = input ? input.value.trim() : '';
          if (!opId) return toast('Enter an operator ID', 'error');
          try {
            await apiFetch('/operator-groups/' + btn.dataset.gid + '/members', {
              method: 'POST',
              body: JSON.stringify({ operator_id: opId })
            });
            toast('Member added');
            await renderProviderGroups(container);
          } catch(e) { toast(e.message, 'error'); }
        };
      });
    } catch(e) { container.innerHTML += '<p style="color:var(--error)">' + esc(e.message) + '</p>'; }
  }

  // --- Provider Rights Tab ---
  async function renderProviderRights(container) {
    container.innerHTML = '<h3>Model Rights</h3><p style="color:var(--text-muted);margin-bottom:12px">Select models and assign access to operators by group.</p>' +
      '<div id="pr-models" style="margin-bottom:16px"></div>' +
      '<button class="btn btn-primary" id="pr-manage-btn" style="margin-bottom:16px" disabled>Manage Rights</button>' +
      '<div id="pr-modal"></div>';

    let selectedModels = [];
    try {
      const models = await apiFetch('/models');
      const myModels = models.filter(m => m.owner_id);
      const modelsEl = document.getElementById('pr-models');
      if (!myModels.length) {
        modelsEl.innerHTML = '<p style="color:var(--text-muted)">Register models first in the "My Models" tab.</p>';
        return;
      }
      modelsEl.innerHTML = myModels.map(m => `
        <label style="display:flex;align-items:center;gap:8px;padding:4px 0;cursor:pointer">
          <input type="checkbox" class="pr-model-cb" value="${esc(m.model_id)}">
          <span>${esc(m.display_name || m.model_id)}</span>
          <span style="font-size:11px;color:var(--text-muted)">(${esc(m.provider)})</span>
        </label>
      `).join('');

      modelsEl.addEventListener('change', () => {
        selectedModels = Array.from(modelsEl.querySelectorAll('.pr-model-cb:checked')).map(cb => cb.value);
        document.getElementById('pr-manage-btn').disabled = selectedModels.length === 0;
      });

      document.getElementById('pr-manage-btn').onclick = () => showRightsModal(container, selectedModels);
    } catch(_) {}
  }

  async function showRightsModal(container, modelIDs) {
    const modal = document.getElementById('pr-modal');
    modal.innerHTML = '<p style="color:var(--text-muted)">Loading operators and groups...</p>';

    try {
      const [groups, ungrouped, grants] = await Promise.all([
        apiFetch('/operator-groups'),
        apiFetch('/operator-groups/ungrouped'),
        apiFetch('/model-grants'),
      ]);

      // Build a set of currently granted operator IDs for these models
      const grantedSet = new Set();
      for (const g of grants) {
        if (g.effect === 'allow' && g.grantee_type === 'user' && modelIDs.includes(g.model_id)) {
          grantedSet.add(g.grantee_id);
        }
      }

      // Load group members
      const groupMembers = {};
      for (const g of groups) {
        try { groupMembers[g.group_id] = await apiFetch('/operator-groups/' + g.group_id + '/members'); } catch(_) { groupMembers[g.group_id] = []; }
      }

      let html = '<div style="border:1px solid var(--border);border-radius:4px;padding:16px">';
      html += '<h4 style="margin-bottom:12px">Assign rights for ' + modelIDs.length + ' model(s)</h4>';
      html += '<div style="display:flex;gap:24px;flex-wrap:wrap">';

      // Columns per group
      for (const g of groups) {
        const members = groupMembers[g.group_id] || [];
        html += `<div style="min-width:200px"><h5 style="margin-bottom:8px">${esc(g.name)}</h5>`;
        html += `<label style="display:flex;align-items:center;gap:4px;margin-bottom:4px;font-weight:bold;cursor:pointer"><input type="checkbox" class="pr-group-all" data-gid="${esc(g.group_id)}" checked> Select all</label>`;
        for (const m of members) {
          const checked = grantedSet.has(m.operator_id) ? 'checked' : '';
          html += `<label style="display:flex;align-items:center;gap:4px;padding:2px 0;cursor:pointer"><input type="checkbox" class="pr-op-cb" data-op="${esc(m.operator_id)}" ${checked}> ${esc(m.operator_id)}</label>`;
        }
        html += '</div>';
      }

      // Ungrouped column
      if (ungrouped.length > 0) {
        html += '<div style="min-width:200px"><h5 style="margin-bottom:8px">Ungrouped</h5>';
        for (const opId of ungrouped) {
          const checked = grantedSet.has(opId) ? 'checked' : '';
          html += `<label style="display:flex;align-items:center;gap:4px;padding:2px 0;cursor:pointer"><input type="checkbox" class="pr-op-cb" data-op="${esc(opId)}" ${checked}> ${esc(opId)}</label>`;
        }
        html += '</div>';
      }

      html += '</div>';
      html += '<div style="margin-top:16px;display:flex;gap:8px"><button class="btn btn-primary" id="pr-submit-btn">Apply Rights</button><button class="btn" id="pr-cancel-btn">Cancel</button></div>';
      html += '</div>';
      modal.innerHTML = html;

      // Group "select all" toggles
      modal.querySelectorAll('.pr-group-all').forEach(cb => {
        cb.onchange = () => {
          const gid = cb.dataset.gid;
          const members = groupMembers[gid] || [];
          const opIds = members.map(m => m.operator_id);
          modal.querySelectorAll('.pr-op-cb').forEach(opcb => {
            if (opIds.includes(opcb.dataset.op)) opcb.checked = cb.checked;
          });
        };
      });

      document.getElementById('pr-cancel-btn').onclick = () => { modal.innerHTML = ''; };
      document.getElementById('pr-submit-btn').onclick = async () => {
        const checked = Array.from(modal.querySelectorAll('.pr-op-cb:checked')).map(cb => cb.dataset.op);
        const unchecked = Array.from(modal.querySelectorAll('.pr-op-cb:not(:checked)')).map(cb => cb.dataset.op);
        try {
          await apiFetch('/model-grants/bulk', {
            method: 'POST',
            body: JSON.stringify({
              model_ids: modelIDs,
              grant_operator_ids: checked,
              revoke_operator_ids: unchecked,
            })
          });
          toast('Rights updated');
          modal.innerHTML = '';
        } catch(e) { toast(e.message, 'error'); }
      };
    } catch(e) { modal.innerHTML = '<p style="color:var(--error)">' + esc(e.message) + '</p>'; }
  }

  async function loadWorkflowList(role) {
    const container = document.getElementById('wf-list');
    if (!container) return;
    try {
      const workflows = await apiFetch('/workflows');
      if (!workflows || !workflows.length) {
        container.innerHTML = '<div class="empty-state"><h3>No workflows</h3></div>';
        return;
      }
      container.innerHTML = '<table class="vacf-table"><thead><tr>' +
        '<th>Name</th><th>Type</th><th>Status</th><th>Steps</th><th>Actions</th>' +
      '</tr></thead><tbody>' +
      workflows.map(w => `<tr>
        <td><a href="#" class="wf-detail-link" data-wfid="${esc(w.workflow_id)}">${esc(w.name)}</a></td>
        <td><span class="wf-type-badge">${esc(w.workflow_type)}</span></td>
        <td><span class="wf-status wf-status-${w.status}">${w.status}</span></td>
        <td>${(w.steps || []).length}</td>
        <td>
          ${w.status === 'active' ? '<button class="btn btn-sm wf-run-btn" data-wfid="' + esc(w.workflow_id) + '">Run</button>' : ''}
          ${w.status === 'draft' ? '<button class="btn btn-sm wf-submit-btn" data-wfid="' + esc(w.workflow_id) + '">Submit</button>' : ''}
          ${role === 'operator' && (w.status === 'validated' || w.status === 'draft') ? '<button class="btn btn-sm btn-primary wf-activate-btn" data-wfid="' + esc(w.workflow_id) + '">Activate</button>' : ''}
        </td>
      </tr>`).join('') +
      '</tbody></table>';

      container.addEventListener('click', async e => {
        const detail = e.target.closest('.wf-detail-link');
        if (detail) { e.preventDefault(); await showWorkflowDetail(detail.dataset.wfid, role); return; }
        const run = e.target.closest('.wf-run-btn');
        if (run) { await showRunDialog(run.dataset.wfid); return; }
        const submit = e.target.closest('.wf-submit-btn');
        if (submit) { await submitWorkflow(submit.dataset.wfid); return; }
        const activate = e.target.closest('.wf-activate-btn');
        if (activate) { await activateWorkflow(activate.dataset.wfid); return; }
      });
    } catch(e) {
      container.innerHTML = '<div class="empty-state"><h3>Error loading workflows</h3><p>' + esc(e.message) + '</p></div>';
    }
  }

  async function loadModelsList() {
    const container = document.getElementById('wf-models');
    if (!container) return;
    try {
      const models = await apiFetch('/models');
      container.innerHTML = '<h3>Available Models</h3>' +
        '<table class="vacf-table"><thead><tr><th>Provider</th><th>Model</th><th>Available</th></tr></thead><tbody>' +
        models.map(m => `<tr>
          <td>${esc(m.provider)}</td>
          <td>${esc(m.model_name)}</td>
          <td>${m.is_available ? 'Yes' : 'No'}</td>
        </tr>`).join('') +
        '</tbody></table>';
    } catch(_) {}
  }

  async function loadGrantsList() {
    const container = document.getElementById('wf-grants');
    if (!container) return;
    try {
      const grants = await apiFetch('/model-grants');
      const models = await apiFetch('/models?available=false');
      const modelOptions = models.map(m => `<option value="${esc(m.model_id)}">${esc(m.model_id)}</option>`).join('');

      container.innerHTML = '<h3>Model Grants</h3>' +
        '<div style="margin-bottom:12px;padding:12px;border:1px solid var(--border);border-radius:4px">' +
          '<div style="display:flex;gap:8px;flex-wrap:wrap;align-items:end">' +
            '<div><label style="display:block;font-size:12px">Grantee Type</label><select id="grant-type" class="search-input" style="width:90px"><option value="user">user</option><option value="role">role</option></select></div>' +
            '<div><label style="display:block;font-size:12px">Grantee ID</label><input id="grant-id" class="search-input" style="width:140px" placeholder="user_id or role"></div>' +
            '<div><label style="display:block;font-size:12px">Model</label><select id="grant-model" class="search-input" style="width:220px"><option value="*">* (all)</option>' + modelOptions + '</select></div>' +
            '<div><label style="display:block;font-size:12px">Step Type</label><select id="grant-step" class="search-input" style="width:90px"><option value="*">*</option><option value="llm">llm</option><option value="sql">sql</option><option value="http">http</option><option value="check">check</option></select></div>' +
            '<div><label style="display:block;font-size:12px">Effect</label><select id="grant-effect" class="search-input" style="width:90px"><option value="allow">allow</option><option value="deny">deny</option></select></div>' +
            '<button class="btn btn-primary btn-sm" id="grant-add-btn">Add Grant</button>' +
          '</div>' +
        '</div>' +
        (grants.length === 0 ? '<p style="color:var(--text-muted)">No grants configured. Without grants, the existing role-based step type permissions apply.</p>' :
        '<table class="vacf-table"><thead><tr><th>Type</th><th>Grantee</th><th>Model</th><th>Step</th><th>Effect</th><th>Actions</th></tr></thead><tbody>' +
        grants.map(g => `<tr>
          <td>${esc(g.grantee_type)}</td>
          <td>${esc(g.grantee_id)}</td>
          <td>${esc(g.model_id)}</td>
          <td>${esc(g.step_type)}</td>
          <td><span style="color:${g.effect === 'deny' ? 'var(--error,red)' : 'var(--success,green)'}">${g.effect}</span></td>
          <td><button class="btn btn-sm grant-del-btn" data-gid="${esc(g.grant_id)}">Delete</button></td>
        </tr>`).join('') +
        '</tbody></table>');

      document.getElementById('grant-add-btn').onclick = async () => {
        try {
          await apiFetch('/model-grants', {
            method: 'POST',
            body: JSON.stringify({
              grantee_type: document.getElementById('grant-type').value,
              grantee_id: document.getElementById('grant-id').value,
              model_id: document.getElementById('grant-model').value,
              step_type: document.getElementById('grant-step').value,
              effect: document.getElementById('grant-effect').value,
            })
          });
          toast('Grant created');
          await loadGrantsList();
        } catch(e) { toast(e.message, 'error'); }
      };

      container.querySelectorAll('.grant-del-btn').forEach(btn => {
        btn.onclick = async () => {
          try {
            await apiFetch('/model-grants/' + btn.dataset.gid, { method: 'DELETE' });
            toast('Grant deleted');
            await loadGrantsList();
          } catch(e) { toast(e.message, 'error'); }
        };
      });
    } catch(_) {}
  }

  async function showWorkflowDetail(wfId, role) {
    try {
      const wf = await apiFetch('/workflows/' + wfId);
      const container = document.getElementById('wf-list');
      const steps = wf.steps || [];
      container.innerHTML = `
        <div style="margin-bottom:16px">
          <button class="btn btn-sm" id="wf-back-btn">Back to list</button>
          <h3 style="display:inline;margin-left:12px">${esc(wf.name)}</h3>
          <span class="wf-status wf-status-${wf.status}" style="margin-left:8px">${wf.status}</span>
        </div>
        <p style="color:var(--text-muted);margin-bottom:16px">${esc(wf.description || '')}</p>
        <h4>VACF Steps</h4>
        <table class="vacf-table vacf-steps"><thead><tr>
          <th>Order</th><th>Name</th><th>Type</th><th>Provider</th><th>Model</th><th>Prompt</th>
        </tr></thead><tbody>
        ${steps.map(s => `<tr>
          <td>${s.step_order}</td>
          <td>${esc(s.step_name)}</td>
          <td><span class="step-type-badge" style="background:${STEP_TYPE_COLORS[s.step_type] || 'var(--border)'}">${s.step_type}</span></td>
          <td>${esc(s.provider || '-')}</td>
          <td>${esc(s.model || '-')}</td>
          <td class="prompt-cell">${esc((s.prompt_template || '').slice(0, 80))}${(s.prompt_template || '').length > 80 ? '...' : ''}</td>
        </tr>`).join('')}
        </tbody></table>
        ${wf.status === 'draft' ? '<button class="btn btn-sm" id="wf-add-step-btn" style="margin-top:8px">Add Step</button>' : ''}
      `;
      document.getElementById('wf-back-btn').onclick = () => loadWorkflowList(role);
      const addBtn = document.getElementById('wf-add-step-btn');
      if (addBtn) addBtn.onclick = () => showStepEditor(wfId, null, role);
    } catch(e) { toast(e.message, 'error'); }
  }

  function showWorkflowEditor(wf, role) {
    const container = document.getElementById('wf-list');
    const allowedTypes = role === 'operator' ? ['llm','check'] : role === 'provider' ? ['llm','check','sql'] : STEP_TYPES;
    container.innerHTML = `
      <h3>${wf ? 'Edit' : 'New'} Workflow</h3>
      <div class="form-group"><label>Name</label><input id="wf-name" class="search-input" value="${esc((wf||{}).name||'')}" style="width:100%"></div>
      <div class="form-group"><label>Type</label><select id="wf-type" class="search-input" style="width:100%">
        ${WF_TYPES.map(t => `<option value="${t}" ${(wf||{}).workflow_type === t ? 'selected' : ''}>${t}</option>`).join('')}
      </select></div>
      <div class="form-group"><label>Description</label><textarea id="wf-desc" class="search-input" rows="2" style="width:100%">${esc((wf||{}).description||'')}</textarea></div>
      <div class="form-group"><label>Pre-prompt template</label><textarea id="wf-preprompt" class="search-input" rows="2" style="width:100%">${esc((wf||{}).pre_prompt_template||'')}</textarea></div>
      <button class="btn btn-primary" id="wf-save-btn">${wf ? 'Update' : 'Create'}</button>
      <button class="btn" id="wf-cancel-btn">Cancel</button>
    `;
    document.getElementById('wf-cancel-btn').onclick = () => loadWorkflowList(role);
    document.getElementById('wf-save-btn').onclick = async () => {
      const body = {
        name: document.getElementById('wf-name').value,
        workflow_type: document.getElementById('wf-type').value,
        description: document.getElementById('wf-desc').value,
        pre_prompt_template: document.getElementById('wf-preprompt').value,
      };
      try {
        if (wf) {
          await apiFetch('/workflows/' + wf.workflow_id, { method: 'PUT', body: JSON.stringify(body) });
        } else {
          await apiFetch('/workflows', { method: 'POST', body: JSON.stringify(body) });
        }
        toast('Workflow saved');
        loadWorkflowList(role);
      } catch(e) { toast(e.message, 'error'); }
    };
  }

  async function showStepEditor(wfId, step, role) {
    const container = document.getElementById('wf-list');
    const allowedTypes = role === 'operator' ? ['llm','check'] : role === 'provider' ? ['llm','check','sql'] : STEP_TYPES;

    // Load allowed models for dropdown
    let modelOptions = '<option value="">-- no model --</option>';
    try {
      const models = await apiFetch('/my-allowed-models');
      modelOptions += models.map(m =>
        `<option value="${esc(m.model_id)}" ${(step||{}).model === m.model_id ? 'selected' : ''}>${esc(m.display_name || m.model_id)}</option>`
      ).join('');
    } catch(_) {}

    container.innerHTML = `
      <h3>${step ? 'Edit' : 'Add'} Step</h3>
      <div class="form-group"><label>Name</label><input id="step-name" class="search-input" value="${esc((step||{}).step_name||'')}" style="width:100%"></div>
      <div class="form-group"><label>Order</label><input id="step-order" type="number" class="search-input" value="${(step||{}).step_order||1}" style="width:80px"></div>
      <div class="form-group"><label>Type</label><select id="step-type" class="search-input">
        ${allowedTypes.map(t => `<option value="${t}" ${(step||{}).step_type === t ? 'selected' : ''}>${t}</option>`).join('')}
      </select></div>
      <div class="form-group"><label>Provider</label><input id="step-provider" class="search-input" value="${esc((step||{}).provider||'')}" style="width:100%"></div>
      <div class="form-group"><label>Model</label><select id="step-model" class="search-input" style="width:100%">${modelOptions}</select></div>
      <div class="form-group"><label>Prompt Template</label><textarea id="step-prompt" class="search-input" rows="4" style="width:100%">${esc((step||{}).prompt_template||'')}</textarea></div>
      <div class="form-group"><label>System Prompt</label><textarea id="step-system" class="search-input" rows="2" style="width:100%">${esc((step||{}).system_prompt||'')}</textarea></div>
      <button class="btn btn-primary" id="step-save-btn">Save Step</button>
      <button class="btn" id="step-cancel-btn">Cancel</button>
    `;
    document.getElementById('step-cancel-btn').onclick = () => showWorkflowDetail(wfId, role);
    document.getElementById('step-save-btn').onclick = async () => {
      const body = {
        step_order: parseInt(document.getElementById('step-order').value) || 1,
        step_name: document.getElementById('step-name').value,
        step_type: document.getElementById('step-type').value,
        provider: document.getElementById('step-provider').value,
        model: document.getElementById('step-model').value,
        prompt_template: document.getElementById('step-prompt').value,
        system_prompt: document.getElementById('step-system').value,
      };
      try {
        if (step) {
          await apiFetch('/workflows/' + wfId + '/steps/' + step.step_id, { method: 'PUT', body: JSON.stringify(body) });
        } else {
          await apiFetch('/workflows/' + wfId + '/steps', { method: 'POST', body: JSON.stringify(body) });
        }
        toast('Step saved');
        showWorkflowDetail(wfId, role);
      } catch(e) { toast(e.message, 'error'); }
    };
  }

  async function showRunDialog(wfId) {
    const body = prompt('Enter body text (or leave empty for node-based run):');
    const prePrompt = prompt('Enter pre-prompt directive (optional):');
    try {
      await apiFetch('/workflows/' + wfId + '/run', {
        method: 'POST',
        body: JSON.stringify({ body: body || '', pre_prompt: prePrompt || '' })
      });
      toast('Workflow run started');
    } catch(e) { toast(e.message, 'error'); }
  }

  async function submitWorkflow(wfId) {
    try {
      await apiFetch('/workflows/' + wfId + '/submit', { method: 'POST' });
      toast('Submitted for validation');
      loadWorkflowList('admin');
    } catch(e) { toast(e.message, 'error'); }
  }

  async function activateWorkflow(wfId) {
    try {
      await apiFetch('/workflows/' + wfId + '/activate', { method: 'POST' });
      toast('Workflow activated');
      loadWorkflowList('admin');
    } catch(e) { toast(e.message, 'error'); }
  }

})();
