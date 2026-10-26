// Shared report-card renderer + upvote wiring + Priority & SLA engine.
// Visual identity: Campus Fresh teal & coral with dynamic intelligence layers.

const STEPS_ALL = ['reported', 'verified', 'in_progress', 'resolved'];

function modalTimelineHtml(status){
  const idx = STEPS_ALL.indexOf(status);
  return `
    <div class="timeline modal-timeline">
      ${STEPS_ALL.map((s, i) => `
        <div class="step ${i <= idx ? 'done' : ''} ${i === idx ? 'current' : ''}">
          <span class="step-label">${STATUS_LABEL[s]}</span>
        </div>
      `).join('')}
    </div>`;
}

// Shimmer loading placeholders for modern SaaS feel
function renderSkeletons(count = 3){
  return Array.from({length: count}).map(() => `
    <div class="skeleton-card">
      <div class="skeleton-thumb"></div>
      <div class="skeleton-body">
        <div style="display:flex; justify-content:space-between; margin-bottom:4px;">
          <div class="skeleton-line" style="width:35%; height:12px;"></div>
          <div class="skeleton-line" style="width:25%; height:12px;"></div>
        </div>
        <div class="skeleton-line" style="width:85%; height:18px;"></div>
        <div class="skeleton-line" style="width:55%; height:14px;"></div>
        <div class="skeleton-line" style="width:95%; height:12px;"></div>
        <div class="skeleton-line" style="width:40%; height:28px; margin-top:8px;"></div>
      </div>
    </div>
  `).join('');
}

// Renders the photo area of a card. Falls back to a themed placeholder
function thumbHtml(r, extraStyle = ''){
  const m = categoryMeta(r.category);
  if (r.image_url) {
    return `<div class="thumb" style="background-image:url('${escapeHtml(r.image_url)}');${extraStyle}">${statusTag(r.status)}</div>`;
  }
  return `
    <div class="thumb thumb-empty" style="--cat-color-pale:${m.color}22;${extraStyle}">
      ${statusTag(r.status)}
      <div class="thumb-empty-inner"><span class="ic">${m.icon}</span><span>Photo not provided</span></div>
    </div>`;
}

function renderReportCard(r){
  const m = categoryMeta(r.category);

  return `
  <article class="card" style="--cat-color:${m.color}" data-id="${r.id}">
    ${thumbHtml(r)}
    <div class="body">
      <h3>${escapeHtml(r.title)}</h3>
      <div class="meta">
        <span>📍 <b>${escapeHtml(r.building)}</b></span>
        <span class="dot">${escapeHtml(r.location)}</span>
      </div>
      <p class="desc">${escapeHtml(r.description).slice(0, 110)}${r.description.length > 110 ? '…' : ''}</p>
      <div class="foot">
        <div style="display:flex; align-items:center; gap:8px;">
          <button class="upvote-btn ${r.has_upvoted ? 'voted' : ''}" data-report-id="${r.id}" ${API.isLoggedIn() ? '' : 'title="Log in to upvote"'}>
            <span class="tri">▲</span> <span class="count">${r.upvote_count}</span>
          </button>
          <button class="btn btn-ghost btn-sm view-details-btn" data-view-id="${r.id}">View Details →</button>
        </div>
        <span class="meta">${categoryChip(r.category)} · ${timeAgo(r.created_at)}</span>
      </div>
    </div>
  </article>`;
}

// Delegated click handler for upvote buttons inside a container.
function attachUpvoteHandlers(container){
  container.querySelectorAll('.upvote-btn').forEach(btn => {
    btn.addEventListener('click', async (e) => {
      e.stopPropagation();
      if (!API.isLoggedIn()) {
        showToast('Please log in to upvote campus issues', 'info');
        setTimeout(() => window.location.href = 'login.html', 1000);
        return;
      }
      const id = btn.dataset.reportId;
      btn.disabled = true;
      try {
        const res = await API.postForm(`/api/reports/${id}/upvote`, new FormData());
        btn.classList.toggle('voted', res.upvoted);
        btn.querySelector('.count').textContent = res.upvote_count;
        showToast(res.upvoted ? 'Upvote registered! (+1)' : 'Upvote removed', 'success');
      } catch (e) {
        showToast(e.message, 'error');
      } finally {
        btn.disabled = false;
      }
    });
  });
}

function attachFeedHandlers(container){
  attachUpvoteHandlers(container);
  container.querySelectorAll('.view-details-btn').forEach(btn => {
    btn.addEventListener('click', (e) => {
      e.stopPropagation();
      openIssueModal(btn.dataset.viewId);
    });
  });
  container.querySelectorAll('.thumb').forEach(thumb => {
    thumb.style.cursor = 'pointer';
    thumb.title = 'Click to inspect issue details';
    thumb.addEventListener('click', () => {
      const card = thumb.closest('.card');
      if (card?.dataset.id) openIssueModal(card.dataset.id);
    });
  });
}

async function openIssueModal(id){
  let backdrop = document.getElementById('issue-modal-backdrop');
  if (!backdrop) {
    backdrop = document.createElement('div');
    backdrop.id = 'issue-modal-backdrop';
    backdrop.className = 'modal-backdrop';
    document.body.appendChild(backdrop);
  }
  backdrop.innerHTML = `
    <div class="modal modal-lg">
      <div style="text-align:center; padding:40px;"><p>Loading issue intelligence…</p></div>
    </div>`;
  backdrop.classList.remove('hidden');

  try {
    const { report: r } = await API.get(`/api/reports/${id}`);
    const m = (typeof categoryMeta === 'function') ? categoryMeta(r.category) : { icon: '📍', color: 'var(--teal)' };
    const catIcon = m?.icon || '📍';

    const beforePhotoHtml = r.image_url
      ? `<img src="${escapeHtml(r.image_url)}" alt="Before Resolution" class="ba-img">`
      : `<div class="ba-placeholder"><span class="ic">${catIcon}</span><span class="ba-text">Photo not provided with report</span></div>`;

    const afterPhotoHtml = r.resolved_image_url
      ? `<img src="${escapeHtml(r.resolved_image_url)}" alt="After Resolution" class="ba-img">`
      : `<div class="ba-placeholder"><span class="ic">${r.status === 'resolved' ? '✅' : '🔧'}</span><span class="ba-text">${r.status === 'resolved' ? 'Resolved by maintenance staff' : 'Awaiting maintenance resolution'}</span></div>`;

    backdrop.innerHTML = `
      <div class="modal modal-lg" style="--cat-color:${m.color}">
        <div class="modal-head">
          <div style="display:flex; align-items:center; gap:8px; flex-wrap:wrap;">
            ${categoryChip(r.category)}
            ${statusTag(r.status)}
          </div>
          <button class="btn btn-ghost btn-sm modal-close" id="issue-modal-close" aria-label="Close">✕</button>
        </div>

        <h2 style="margin:14px 0 6px; font-size:1.35rem; line-height:1.3; color:var(--ink);">${escapeHtml(r.title)}</h2>
        <div class="meta" style="margin-bottom:16px;">
          <span>📍 <b>${escapeHtml(r.building)}</b></span>
          <span class="dot">${escapeHtml(r.location)}</span>
          <span class="dot">Reported ${timeAgo(r.created_at)}</span>
        </div>

        ${modalTimelineHtml(r.status)}

        <div class="before-after-grid">
          <div class="before-after-card">
            <div class="ba-label label-before">📷 Before Resolution</div>
            <div class="ba-view">${beforePhotoHtml}</div>
          </div>
          <div class="before-after-card">
            <div class="ba-label label-after">✅ After Resolution</div>
            <div class="ba-view">${afterPhotoHtml}</div>
          </div>
        </div>

        <div style="margin-top:18px;">
          <h4 style="font-size:0.85rem; text-transform:uppercase; letter-spacing:0.04em; color:var(--muted); margin-bottom:6px;">Detailed Description</h4>
          <p style="margin:0; font-size:0.95rem; line-height:1.6; color:var(--ink); background:var(--paper-2); padding:14px 16px; border-radius:var(--radius-sm);">${escapeHtml(r.description)}</p>
        </div>

        <div class="modal-foot" style="margin-top:22px; display:flex; justify-content:space-between; align-items:center; flex-wrap:wrap; gap:12px; border-top:1px solid var(--line); padding-top:16px;">
          <button class="modal-upvote-btn upvote-btn ${r.has_upvoted ? 'voted' : ''}" id="modal-upvote-btn" data-report-id="${r.id}" ${API.isLoggedIn() ? '' : 'title="Log in to upvote"'}>
            <span class="tri">▲</span> <span class="count">${r.upvote_count}</span> <span style="font-size:0.88rem; margin-left:4px; font-weight:700;">Upvote Issue</span>
          </button>
          <button class="modal-close-btn" id="modal-close-btn">Close</button>
        </div>
      </div>`;

    document.getElementById('issue-modal-close').onclick = () => backdrop.classList.add('hidden');
    document.getElementById('modal-close-btn').onclick = () => backdrop.classList.add('hidden');
    backdrop.onclick = (e) => { if (e.target === backdrop) backdrop.classList.add('hidden'); };

    const modalUpvoteBtn = document.getElementById('modal-upvote-btn');
    modalUpvoteBtn.onclick = async () => {
      if (!API.isLoggedIn()) {
        showToast('Please log in to upvote', 'info');
        setTimeout(() => window.location.href = 'login.html', 1000);
        return;
      }
      modalUpvoteBtn.disabled = true;
      try {
        const res = await API.postForm(`/api/reports/${r.id}/upvote`, new FormData());
        modalUpvoteBtn.classList.toggle('voted', res.upvoted);
        modalUpvoteBtn.querySelector('.count').textContent = res.upvote_count;
        document.querySelectorAll(`.upvote-btn[data-report-id="${r.id}"]`).forEach(b => {
          b.classList.toggle('voted', res.upvoted);
          b.querySelector('.count').textContent = res.upvote_count;
        });
        showToast(res.upvoted ? 'Upvote registered!' : 'Upvote removed', 'success');
      } catch (e) {
        showToast(e.message, 'error');
      } finally {
        modalUpvoteBtn.disabled = false;
      }
    };
  } catch (err) {
    backdrop.innerHTML = `
      <div class="modal modal-lg">
        <p style="color:var(--coral-deep)">Failed to load details: ${escapeHtml(err.message)}</p>
        <button class="btn btn-ghost btn-sm" onclick="document.getElementById('issue-modal-backdrop').classList.add('hidden')">Close</button>
      </div>`;
  }
}

// Auto-open modal if URL query specifies ?open=ID or ?issue=ID
window.addEventListener('load', () => {
  const p = new URLSearchParams(window.location.search);
  const targetId = p.get('open') || p.get('issue');
  if (targetId) {
    setTimeout(() => openIssueModal(targetId), 250);
  }
});
