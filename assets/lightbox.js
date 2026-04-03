(function () {
  'use strict';

  var gallery = [];
  var currentIndex = 0;
  var overlay, lbImg, prevBtn, nextBtn;

  function buildOverlay() {
    overlay = document.createElement('div');
    overlay.className = 'lb-overlay';
    overlay.setAttribute('role', 'dialog');
    overlay.setAttribute('aria-modal', 'true');
    overlay.setAttribute('aria-label', 'Image lightbox');
    overlay.setAttribute('tabindex', '-1');

    lbImg = document.createElement('img');
    lbImg.className = 'lb-img';
    lbImg.alt = '';

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

    overlay.append(closeBtn, prevBtn, lbImg, nextBtn);

    overlay.addEventListener('click', function (e) {
      if (e.target === overlay) closeLightbox();
    });

    document.body.appendChild(overlay);
  }

  function openLightbox(index) {
    currentIndex = index;
    updateNav();
    lbImg.src = gallery[index].src;
    lbImg.alt = gallery[index].alt || '';
    overlay.classList.add('lb-open');
    document.body.style.overflow = 'hidden';
    overlay.focus();
  }

  function closeLightbox() {
    overlay.classList.remove('lb-open');
    document.body.style.overflow = '';
  }

  function navigate(dir) {
    if (gallery.length <= 1) return;
    var next = (currentIndex + dir + gallery.length) % gallery.length;
    lbImg.classList.add('lb-switching');
    setTimeout(function () {
      currentIndex = next;
      lbImg.src = gallery[next].src;
      lbImg.alt = gallery[next].alt || '';
      lbImg.classList.remove('lb-switching');
      updateNav();
    }, 150);
  }

  function updateNav() {
    var single = gallery.length <= 1;
    prevBtn.style.display = single ? 'none' : '';
    nextBtn.style.display = single ? 'none' : '';
  }

  function init() {
    buildOverlay();

    var images = document.querySelectorAll('.responsive-image[data-lightbox-src]');

    gallery = Array.from(images).map(function (el) {
      return { src: el.dataset.lightboxSrc, alt: el.alt };
    });

    images.forEach(function (el, i) {
      // Fade in once the thumbnail is loaded
      if (el.complete && el.naturalWidth > 0) {
        el.classList.add('lb-loaded');
      } else {
        el.addEventListener('load', function () { el.classList.add('lb-loaded'); });
      }

      // The photos-page wraps each image in <a href="/post/">; intercept it.
      // On post pages the image may not have an anchor wrapper.
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
      if (e.key === 'Escape')      closeLightbox();
      if (e.key === 'ArrowLeft')   navigate(-1);
      if (e.key === 'ArrowRight')  navigate(1);
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
