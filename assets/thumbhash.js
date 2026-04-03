// ThumbHash decoder — https://github.com/evanw/thumbhash (MIT)
// Adapted from the ESM source: export keywords removed for bundle inclusion.

function thumbHashToRGBA(hash) {
  var PI = Math.PI, min = Math.min, max = Math.max, cos = Math.cos, round = Math.round;

  var header24 = hash[0] | (hash[1] << 8) | (hash[2] << 16);
  var header16 = hash[3] | (hash[4] << 8);
  var l_dc = (header24 & 63) / 63;
  var p_dc = ((header24 >> 6) & 63) / 31.5 - 1;
  var q_dc = ((header24 >> 12) & 63) / 31.5 - 1;
  var l_scale = ((header24 >> 18) & 31) / 31;
  var hasAlpha = header24 >> 23;
  var p_scale = ((header16 >> 3) & 63) / 63;
  var q_scale = ((header16 >> 9) & 63) / 63;
  var isLandscape = header16 >> 15;
  var lx = max(3, isLandscape ? (hasAlpha ? 5 : 7) : (header16 & 7));
  var ly = max(3, isLandscape ? (header16 & 7) : (hasAlpha ? 5 : 7));
  var a_dc = hasAlpha ? (hash[5] & 15) / 15 : 1;
  var a_scale = (hash[5] >> 4) / 15;

  var ac_start = hasAlpha ? 6 : 5;
  var ac_index = 0;
  function decodeChannel(nx, ny, scale) {
    var ac = [];
    for (var cy = 0; cy < ny; cy++)
      for (var cx = cy ? 0 : 1; cx * ny < nx * (ny - cy); cx++)
        ac.push((((hash[ac_start + (ac_index >> 1)] >> ((ac_index++ & 1) << 2)) & 15) / 7.5 - 1) * scale);
    return ac;
  }
  var l_ac = decodeChannel(lx, ly, l_scale);
  var p_ac = decodeChannel(3, 3, p_scale * 1.25);
  var q_ac = decodeChannel(3, 3, q_scale * 1.25);
  var a_ac = hasAlpha ? decodeChannel(5, 5, a_scale) : null;

  var ratio = thumbHashToApproximateAspectRatio(hash);
  var w = round(ratio > 1 ? 32 : 32 * ratio);
  var h = round(ratio > 1 ? 32 / ratio : 32);
  var rgba = new Uint8Array(w * h * 4);
  var fx = [], fy = [];
  for (var y = 0, i = 0; y < h; y++) {
    for (var x = 0; x < w; x++, i += 4) {
      var l = l_dc, p = p_dc, q = q_dc, a = a_dc;
      var n1 = max(lx, hasAlpha ? 5 : 3);
      for (var cx = 0; cx < n1; cx++) fx[cx] = cos(PI / w * (x + 0.5) * cx);
      var n2 = max(ly, hasAlpha ? 5 : 3);
      for (var cy = 0; cy < n2; cy++) fy[cy] = cos(PI / h * (y + 0.5) * cy);
      for (var cy = 0, j = 0; cy < ly; cy++)
        for (var cx = cy ? 0 : 1, fy2 = fy[cy] * 2; cx * ly < lx * (ly - cy); cx++, j++)
          l += l_ac[j] * fx[cx] * fy2;
      for (var cy = 0, j = 0; cy < 3; cy++) {
        for (var cx = cy ? 0 : 1, fy2 = fy[cy] * 2; cx < 3 - cy; cx++, j++) {
          var f = fx[cx] * fy2;
          p += p_ac[j] * f;
          q += q_ac[j] * f;
        }
      }
      if (hasAlpha)
        for (var cy = 0, j = 0; cy < 5; cy++)
          for (var cx = cy ? 0 : 1, fy2 = fy[cy] * 2; cx < 5 - cy; cx++, j++)
            a += a_ac[j] * fx[cx] * fy2;
      var b = l - 2 / 3 * p;
      var r = (3 * l - b + q) / 2;
      var g = r - q;
      rgba[i]     = max(0, 255 * min(1, r));
      rgba[i + 1] = max(0, 255 * min(1, g));
      rgba[i + 2] = max(0, 255 * min(1, b));
      rgba[i + 3] = max(0, 255 * min(1, a));
    }
  }
  return { w: w, h: h, rgba: rgba };
}

function thumbHashToApproximateAspectRatio(hash) {
  var header = hash[3];
  var hasAlpha = hash[2] & 0x80;
  var isLandscape = hash[4] & 0x80;
  var lx = isLandscape ? (hasAlpha ? 5 : 7) : (header & 7);
  var ly = isLandscape ? (header & 7) : (hasAlpha ? 5 : 7);
  return lx / ly;
}

function rgbaToDataURL(w, h, rgba) {
  var row = w * 4 + 1;
  var idat = 6 + h * (5 + row);
  var bytes = [
    137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0,
    w >> 8, w & 255, 0, 0, h >> 8, h & 255, 8, 6, 0, 0, 0, 0, 0, 0, 0,
    idat >>> 24, (idat >> 16) & 255, (idat >> 8) & 255, idat & 255,
    73, 68, 65, 84, 120, 1
  ];
  var table = [
    0, 498536548, 997073096, 651767980, 1994146192, 1802195444, 1303535960,
    1342533948, -306674912, -267414716, -690576408, -882789492, -1687895376,
    -2032938284, -1609899400, -1111625188
  ];
  var a = 1, b = 0;
  for (var y = 0, i = 0, end = row - 1; y < h; y++, end += row - 1) {
    bytes.push(y + 1 < h ? 0 : 1, row & 255, row >> 8, ~row & 255, (row >> 8) ^ 255, 0);
    for (b = (b + a) % 65521; i < end; i++) {
      var u = rgba[i] & 255;
      bytes.push(u);
      a = (a + u) % 65521;
      b = (b + a) % 65521;
    }
  }
  bytes.push(
    b >> 8, b & 255, a >> 8, a & 255, 0, 0, 0, 0,
    0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130
  );
  for (var range of [[12, 29], [37, 41 + idat]]) {
    var start = range[0], end = range[1];
    var c = ~0;
    for (var i = start; i < end; i++) {
      c ^= bytes[i];
      c = (c >>> 4) ^ table[c & 15];
      c = (c >>> 4) ^ table[c & 15];
    }
    c = ~c;
    bytes[end++] = c >>> 24;
    bytes[end++] = (c >> 16) & 255;
    bytes[end++] = (c >> 8) & 255;
    bytes[end++] = c & 255;
  }
  return 'data:image/png;base64,' + btoa(String.fromCharCode.apply(null, bytes));
}

function thumbHashToDataURL(hash) {
  var image = thumbHashToRGBA(hash);
  return rgbaToDataURL(image.w, image.h, image.rgba);
}
