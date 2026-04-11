(function () {
  'use strict';

  var gallery = [];
  var currentIndex = 0;
  var overlay, lbPicture, lbSource, lbImg, prevBtn, nextBtn;
  var prefetchCache = {};
  var switchTimer = null;

  function buildOverlay() {
    overlay = document.createElement('div');
    overlay.className = 'lb-overlay';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.setAttribute('aria-label', 'Image lightbox');
    overlay.setAttribute('tabindex', '-1');

    lbPicture = document.createElement('picture');
    lbPicture.className = 'lb-picture';

    lbSource = document.createElement('source');
    lbSource.type = 'image/webp';

    lbImg = document.createElement('img');
    lbImg.className = 'lb-img';
    lbImg.alt = '';

    lbPicture.append(lbSource, lbImg);

    var closeBtn = document.createElement('button');
    closeBtn.className = 'lb-close';
    closeBtn.innerHTML = '&times;';
    closeBtn.setAttribute('aria-label', 'Close');
    closeBtn.addEventListener('click', closeLightbox);

    prevBtn = document.createElement('button');
    prevBtn.className = 'lb-nav lb-prev';
    prevBtn.innerHTML = '&#8249;';
    prevBtn.setAttribute('aria-label', 'Previous image');
    prevBtn.addEventListener('click', function () { navigate(-1); });

    nextBtn = document.createElement('button');
    nextBtn.className = 'lb-nav lb-next';
    nextBtn.innerHTML = '&#8250;';
    nextBtn.setAttribute('aria-label', 'Next image');
    nextBtn.addEventListener('click', function () { navigate(1); });

    overlay.append(closeBtn, prevBtn, lbPicture, nextBtn);

    overlay.addEventListener('click', function (e) {
      if (e.target === overlay) closeLightbox();
    });

    document.body.appendChild(overlay);
  }

  // ─── Prefetch ───────────────────────────────────────────────────────────────

  function prefetchItem(item) {
    if (!item) return;
    if (item.src && !prefetchCache[item.src]) {
      var img = new Image();
      img.src = item.src;
      prefetchCache[item.src] = img;
    }
    if (item.srcJpg && !prefetchCache[item.srcJpg]) {
      var imgJpg = new Image();
      imgJpg.src = item.srcJpg;
      prefetchCache[item.srcJpg] = imgJpg;
    }
  }

  function prefetchAdjacent(index) {
    for (var d = 1; d <= 3; d++) {
      var n = (index + d) % gallery.length;
      var p = (index - d + gallery.length) % gallery.length;
      prefetchItem(gallery[n]);
      if (p !== n) prefetchItem(gallery[p]);
    }
  }

  // ─── Image switching ────────────────────────────────────────────────────────

  function clearHandlers() {
    lbImg.onload = null;
    lbImg.onerror = null;
  }

  function setImage(item) {
    // Set handlers BEFORE changing src to avoid the completion race
    lbImg.onload = function () {
      clearHandlers();
      lbImg.classList.remove('lb-switching');
    };
    lbImg.onerror = function () {
      clearHandlers();
      lbImg.classList.remove('lb-switching');
    };
    lbSource.srcset = item.src || '';
    lbImg.src = item.srcJpg || item.src;
    lbImg.alt = item.alt || '';
  }

  function openLightbox(index) {
    currentIndex = index;
    var item = gallery[index];

    prefetchItem(item);
    clearHandlers();

    // Show the image immediately on open — no fade needed
    lbImg.classList.remove('lb-switching');
    lbSource.srcset = item.src || '';
    lbImg.src = item.srcJpg || item.src;
    lbImg.alt = item.alt || '';

    updateNav();
    overlay.classList.add('lb-open');
    document.body.style.overflow = 'hidden';
    overlay.focus();

    prefetchAdjacent(index);
  }

  function closeLightbox() {
    overlay.classList.remove('lb-open');
    document.body.style.overflow = '';
  }

  function navigate(dir) {
    if (gallery.length <= 1) return;
    var next = (currentIndex + dir + gallery.length) % gallery.length;
    var item = gallery[next];

    // Cancel any pending switch
    if (switchTimer) { clearTimeout(switchTimer); switchTimer = null; }

    // Stop any in-flight load from the previous navigation
    clearHandlers();

    // Kick off prefetch immediately — don't wait for the fade-out timer.
    // Rapid keypresses cancel the timer, so prefetch must fire here or it never runs.
    prefetchItem(item);
    prefetchAdjacent(next);

    // Fade out the current image immediately
    lbImg.classList.add('lb-switching');
    currentIndex = next;
    updateNav();

    // After the fade-out completes, swap source and wait for load
    switchTimer = setTimeout(function () {
      switchTimer = null;
      setImage(item);
    }, 150);
  }

  function updateNav() {
    var single = gallery.length <= 1;
    prevBtn.style.display = single ? 'none' : '';
    nextBtn.style.display = single ? 'none' : '';
  }

  // ─── Init ───────────────────────────────────────────────────────────────────

  // ─── Thumb sizing ───────────────────────────────────────────────────────────

  // If the image's natural dimensions are smaller than the square container in
  // either axis, shrink the container to match so small images aren't upscaled.
  function adjustThumbSize(img) {
    var thumb = img.closest('.photo-thumb');
    if (!thumb) return;
    var nw = img.naturalWidth;
    var nh = img.naturalHeight;
    if (!nw || !nh) return;

    // The square side is the thumb's rendered width (= content column width).
    var limit = thumb.offsetWidth || 680;
    if (nw >= limit && nh >= limit) return; // large image — keep the square

    var tw = Math.min(nw, limit);
    var th = Math.min(nh, limit);
    thumb.style.maxWidth = tw + 'px';
    thumb.style.aspectRatio = tw + '/' + th;
  }

  function init() {
    buildOverlay();

    var images = document.querySelectorAll('.responsive-image[data-lightbox-src]');

    gallery = Array.from(images).map(function (el) {
      return {
        src: el.dataset.lightboxSrc,
        srcJpg: el.dataset.lightboxSrcJpg || null,
        alt: el.alt
      };
    });

    images.forEach(function (el, i) {
      if (el.complete && el.naturalWidth > 0) {
        el.classList.add('lb-loaded');
        adjustThumbSize(el);
      } else {
        el.addEventListener('load', function () {
          el.classList.add('lb-loaded');
          adjustThumbSize(el);
        });
      }

      var anchor = el.closest('a');
      if (anchor) {
        anchor.addEventListener('click', function (e) {
          e.preventDefault();
          openLightbox(i);
        });
      } else {
        el.addEventListener('click', function () { openLightbox(i); });
      }
    });

    document.addEventListener('keydown', function (e) {
      if (!overlay.classList.contains('lb-open')) return;
      if (e.key === 'Escape')     closeLightbox();
      if (e.key === 'ArrowLeft')  navigate(-1);
      if (e.key === 'ArrowRight') navigate(1);
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
