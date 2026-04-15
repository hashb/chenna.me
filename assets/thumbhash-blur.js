(function () {
  'use strict';

  function initThumbHashPlaceholders() {
    document.querySelectorAll('.photo-thumb[data-thumbhash]').forEach(function (wrap) {
      var b64 = wrap.dataset.thumbhash;
      if (!b64) return;

      // Decode base64 → Uint8Array → PNG data URL
      var raw = atob(b64);
      var bytes = new Uint8Array(raw.length);
      for (var i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i);
      var dataURL = thumbHashToDataURL(bytes);

      // Use as background of the wrapper so it shows while the real img loads.
      // Set via CSS custom property so background-size/position from the
      // stylesheet always apply correctly (avoids Safari inline-style cascade issues).
      wrap.style.setProperty('--thumbhash-url', 'url(' + dataURL + ')');
      wrap.classList.add('thumbhash-active');

      var img = wrap.querySelector('img.responsive-image');
      if (!img) return;

      function onLoaded() {
        wrap.classList.add('thumbhash-done');
        // Remove background after fade completes
        setTimeout(function () {
          wrap.style.removeProperty('--thumbhash-url');
          wrap.classList.remove('thumbhash-active', 'thumbhash-done');
        }, 500);
      }

      // lb-loaded is added by lightbox.js when the image finishes loading.
      // We observe the class mutation rather than the 'load' event to stay
      // in sync with the existing opacity transition.
      if (img.classList.contains('lb-loaded')) {
        onLoaded();
      } else {
        var observer = new MutationObserver(function (mutations) {
          mutations.forEach(function (m) {
            if (m.type === 'attributes' && img.classList.contains('lb-loaded')) {
              observer.disconnect();
              onLoaded();
            }
          });
        });
        observer.observe(img, { attributes: true, attributeFilter: ['class'] });
      }
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initThumbHashPlaceholders);
  } else {
    initThumbHashPlaceholders();
  }
})();
