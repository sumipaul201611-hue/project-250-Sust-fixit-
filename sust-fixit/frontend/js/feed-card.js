// Shared report-card renderer + upvote wiring, used by index/feed/dashboard pages.

function renderReportCard(r){
  const img = r.image_url || '';
  return `
  <article class="card" data-id="${r.id}">
    <div class="thumb" style="background-image:url('${escapeHtml(img)}')">
      ${statusTag(r.status)}
    </div>
    <div class="body">
      <h3>${escapeHtml(r.title)}</h3>
      <div class="meta">
        <span>${escapeHtml(r.building)}</span>
        <span class="dot">${escapeHtml(r.location)}</span>
      </div>
      <p class="desc">${escapeHtml(r.description).slice(0, 110)}${r.description.length > 110 ? '…' : ''}</p>
      <div class="foot">
        <button class="upvote-btn ${r.has_upvoted ? 'voted' : ''}" data-report-id="${r.id}" ${API.isLoggedIn() ? '' : 'disabled title="Log in to upvote"'}>
          <span class="tri">▲</span> <span class="count">${r.upvote_count}</span>
        </button>
        <span class="meta">${escapeHtml(r.category)} · ${timeAgo(r.created_at)}</span>
      </div>
    </div>
  </article>`;
}

// Delegated click handler for upvote buttons inside a container.
function attachUpvoteHandlers(container){
  container.querySelectorAll('.upvote-btn').forEach(btn => {
    btn.addEventListener('click', async () => {
      if (!API.isLoggedIn()) { window.location.href = 'login.html'; return; }
      const id = btn.dataset.reportId;
      btn.disabled = true;
      try {
        const res = await API.postForm(`/api/reports/${id}/upvote`, new FormData());
        btn.classList.toggle('voted', res.upvoted);
        btn.querySelector('.count').textContent = res.upvote_count;
      } catch (e) {
        alert(e.message);
      } finally {
        btn.disabled = false;
      }
    });
  });
}
