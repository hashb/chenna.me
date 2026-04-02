(function () {
  var container = document.getElementById('webmentions-container');
  var pageUrl = container.dataset.pageUrl;
  var token = container.dataset.token;

  function esc(str) {
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  fetch('https://webmention.io/api/mentions.jf2?target=' + encodeURIComponent(pageUrl) + '&token=' + token + '&per-page=100')
    .then(function (res) { return res.json(); })
    .then(function (data) {
      var mentions = data.children || [];
      if (mentions.length === 0) {
        container.innerHTML = '<p>No webmentions yet.</p>';
        return;
      }

      var likes   = mentions.filter(function (m) { return m['wm-property'] === 'like-of'; });
      var reposts = mentions.filter(function (m) { return m['wm-property'] === 'repost-of'; });
      var replies = mentions.filter(function (m) { return ['in-reply-to', 'mention-of', 'bookmark-of'].includes(m['wm-property']); });

      var html = '';

      if (likes.length > 0) {
        html += '<p><strong>' + likes.length + ' like' + (likes.length !== 1 ? 's' : '') + '</strong></p>';
        html += '<div class="wm-likes">';
        likes.forEach(function (m) {
          var author = m.author || {};
          var name = esc(author.name || 'Anonymous');
          var url = esc(m.url || m['wm-source'] || '#');
          html += '<a href="' + url + '" title="' + name + '" target="_blank" rel="noopener">';
          if (author.photo) {
            html += '<img src="' + esc(author.photo) + '" alt="' + name + '" width="32" height="32" loading="lazy">';
          } else {
            html += name;
          }
          html += '</a> ';
        });
        html += '</div>';
      }

      if (reposts.length > 0) {
        html += '<p><strong>' + reposts.length + ' repost' + (reposts.length !== 1 ? 's' : '') + '</strong></p>';
        html += '<div class="wm-reposts">';
        reposts.forEach(function (m) {
          var author = m.author || {};
          var name = esc(author.name || 'Anonymous');
          var url = esc(m.url || m['wm-source'] || '#');
          html += '<a href="' + url + '" title="' + name + '" target="_blank" rel="noopener">';
          if (author.photo) {
            html += '<img src="' + esc(author.photo) + '" alt="' + name + '" width="32" height="32" loading="lazy">';
          } else {
            html += name;
          }
          html += '</a> ';
        });
        html += '</div>';
      }

      if (replies.length > 0) {
        html += '<p><strong>' + replies.length + ' repl' + (replies.length !== 1 ? 'ies' : 'y') + '</strong></p>';
        html += '<ul class="wm-replies">';
        replies.forEach(function (m) {
          var author = m.author || {};
          var name = esc(author.name || 'Anonymous');
          var url = esc(m.url || m['wm-source'] || '#');
          var text = m.content && m.content.text ? esc(m.content.text).substring(0, 300) : '';
          html += '<li>';
          if (author.photo) {
            html += '<img src="' + esc(author.photo) + '" alt="' + name + '" width="24" height="24" loading="lazy"> ';
          }
          html += '<a href="' + url + '" target="_blank" rel="noopener">' + name + '</a>';
          if (text) {
            html += '<blockquote>' + text + (m.content.text.length > 300 ? '&hellip;' : '') + '</blockquote>';
          }
          html += '</li>';
        });
        html += '</ul>';
      }

      container.innerHTML = html;
    })
    .catch(function () {
      container.innerHTML = '<p>Failed to load webmentions.</p>';
    });
}());
