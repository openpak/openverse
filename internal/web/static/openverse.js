/* OpenVerse in the browser. Every page works without this; it adds the account menu closing,
 * Yeahs without a reload, copy-link, the notification count and the drawing pad. */
(function () {
  'use strict';
  document.documentElement.classList.add('js');

  // A picture that fails to load (a face that was never rendered) leaves the initial showing.
  document.addEventListener('error', function (e) {
    var el = e.target;
    if (el instanceof HTMLImageElement && el.closest('.av')) { el.remove(); }
  }, true);

  // Account menu: closes on a click elsewhere and on Escape.
  document.addEventListener('click', function (e) {
    document.querySelectorAll('details[data-menu][open]').forEach(function (d) { if (!d.contains(e.target)) { d.open = false; } });
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') { document.querySelectorAll('details[data-menu][open]').forEach(function (d) { d.open = false; }); }
  });

  // Unread count.
  function setUnread(n) {
    document.querySelectorAll('[data-unread]').forEach(function (el) {
      el.textContent = n > 99 ? '99+' : String(n);
      el.hidden = n < 1;
    });
  }
  if (document.querySelector('[data-unread]')) {
    var poll = function () {
      fetch('/notifications/count', { headers: { Accept: 'application/json' }, credentials: 'same-origin' })
        .then(function (r) { return r.ok ? r.json() : null; })
        .then(function (j) { if (j) { setUnread(j.count | 0); } })
        .catch(function () {});
    };
    setInterval(poll, 60000);
  }

  // Yeah without a reload.
  document.addEventListener('submit', function (e) {
    var f = e.target;
    if (f instanceof HTMLFormElement && f.hasAttribute('data-yeah')) {
      e.preventDefault();
      var btn = f.querySelector('button');
      var add = f.querySelector('input[name=add]');
      var on = add.value === '1';
      btn.disabled = true;
      fetch(f.action, { method: 'POST', body: new URLSearchParams(new FormData(f)), headers: { Accept: 'application/json' }, credentials: 'same-origin' })
        .then(function (r) { if (!r.ok) { throw new Error('yeah'); } return r.json(); })
        .then(function (j) {
          btn.setAttribute('aria-pressed', on ? 'true' : 'false');
          btn.querySelector('[data-count]').textContent = String(j.count);
          add.value = on ? '0' : '1';
        })
        .catch(function () { f.submit(); })
        .finally(function () { btn.disabled = false; });
    }
    if (f instanceof HTMLFormElement && f.dataset.confirm && !window.confirm(f.dataset.confirm)) { e.preventDefault(); }
  });

  // Copy link.
  document.addEventListener('click', function (e) {
    var b = e.target instanceof Element ? e.target.closest('[data-share]') : null;
    if (!b) { return; }
    var url = new URL(b.getAttribute('data-share'), location.origin).href;
    var label = b.querySelector('span');
    var done = function () { if (label) { var old = label.textContent; label.textContent = b.getAttribute('data-copied') || 'Copied'; setTimeout(function () { label.textContent = old; }, 1800); } };
    if (navigator.clipboard) { navigator.clipboard.writeText(url).then(done, function () { window.prompt(b.getAttribute('data-prompt') || 'Copy this link', url); }); }
    else { window.prompt(b.getAttribute('data-prompt') || 'Copy this link', url); }
  });

  // Composer: character counter, text/drawing tabs, the pad.
  var W = 320, H = 120;

  function bmp1(ink) {
    var rowBytes = Math.ceil(W / 8), stride = Math.ceil(rowBytes / 4) * 4, pixelBytes = stride * H, head = 14 + 40 + 8;
    var out = new Uint8Array(head + pixelBytes), v = new DataView(out.buffer);
    out[0] = 0x42; out[1] = 0x4d;
    v.setUint32(2, out.length, true); v.setUint32(10, head, true); v.setUint32(14, 40, true);
    v.setInt32(18, W, true); v.setInt32(22, H, true); v.setUint16(26, 1, true); v.setUint16(28, 1, true);
    v.setUint32(34, pixelBytes, true); v.setInt32(38, 2835, true); v.setInt32(42, 2835, true); v.setUint32(46, 2, true);
    out.set([0, 0, 0, 0, 255, 255, 255, 0], 54); // palette: 0 = black, 1 = white
    for (var y = 0; y < H; y++) {
      var row = head + (H - 1 - y) * stride;
      for (var x = 0; x < W; x++) { if (!ink[y * W + x]) { out[row + (x >> 3)] |= 0x80 >> (x & 7); } }
    }
    return out;
  }
  function b64(bytes) {
    var s = '';
    for (var i = 0; i < bytes.length; i += 0x8000) { s += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000)); }
    return btoa(s);
  }
  function stamp(ctx, x, y, pen) {
    ctx.fillStyle = pen === 'eraser' ? '#fff' : '#000';
    if (pen === 'small') { ctx.fillRect(x - 1, y, 3, 1); ctx.fillRect(x, y - 1, 1, 3); }
    else { ctx.fillRect(x - 3, y - 3, 7, 7); ctx.fillRect(x - 4, y - 1, 9, 3); ctx.fillRect(x - 1, y - 4, 3, 9); }
  }
  function line(ctx, x0, y0, x1, y1, pen) {
    var dx = Math.abs(x1 - x0), dy = Math.abs(y1 - y0), sx = x0 < x1 ? 1 : -1, sy = y0 < y1 ? 1 : -1, err = dx - dy;
    for (;;) {
      stamp(ctx, x0, y0, pen);
      if (x0 === x1 && y0 === y1) { return; }
      var e2 = err * 2;
      if (e2 > -dy) { err -= dy; x0 += sx; }
      if (e2 < dx) { err += dx; y0 += sy; }
    }
  }

  document.querySelectorAll('form[data-composer]').forEach(function (form) {
    var body = form.querySelector('[data-body]'), counter = form.querySelector('[data-counter]');
    if (body && counter) {
      var max = parseInt(body.getAttribute('maxlength'), 10) || 280;
      var upd = function () { var left = max - body.value.length; counter.textContent = String(left); counter.classList.toggle('low', left < 20); };
      body.addEventListener('input', upd); upd();
    }
    var canvas = form.querySelector('[data-canvas]');
    if (!canvas) { return; }
    var ctx = canvas.getContext('2d', { willReadFrequently: true });
    var kind = form.querySelector('[data-kind]'), painting = form.querySelector('[data-painting]');
    var tabs = form.querySelector('[data-tabs]'), pane = form.querySelector('[data-pane=painting]'), empty = form.querySelector('[data-empty]');
    var pen = 'small', undo = [], last = null;
    var clear = function () { ctx.fillStyle = '#fff'; ctx.fillRect(0, 0, W, H); };
    var save = function () { undo.push(ctx.getImageData(0, 0, W, H)); if (undo.length > 40) { undo.shift(); } };
    clear();
    tabs.hidden = false;
    var show = function (name) {
      kind.value = name; pane.hidden = name !== 'painting'; empty.hidden = true;
      tabs.querySelectorAll('[data-tab]').forEach(function (t) { t.setAttribute('aria-selected', String(t.dataset.tab === name)); });
    };
    tabs.querySelectorAll('[data-tab]').forEach(function (t) { t.addEventListener('click', function () { show(t.dataset.tab); }); });
    form.querySelectorAll('[data-tool]').forEach(function (t) {
      t.addEventListener('click', function () {
        pen = t.dataset.tool;
        form.querySelectorAll('[data-tool]').forEach(function (o) { o.setAttribute('aria-pressed', String(o === t)); });
      });
    });
    form.querySelector('[data-action=undo]').addEventListener('click', function () { var l = undo.pop(); if (l) { ctx.putImageData(l, 0, 0); } });
    form.querySelector('[data-action=clear]').addEventListener('click', function () { save(); clear(); });
    var at = function (e) {
      var r = canvas.getBoundingClientRect();
      return { x: Math.max(0, Math.min(W - 1, Math.floor((e.clientX - r.left) * W / r.width))), y: Math.max(0, Math.min(H - 1, Math.floor((e.clientY - r.top) * H / r.height))) };
    };
    canvas.addEventListener('pointerdown', function (e) { e.preventDefault(); canvas.setPointerCapture(e.pointerId); save(); last = at(e); stamp(ctx, last.x, last.y, pen); });
    canvas.addEventListener('pointermove', function (e) { if (!last) { return; } var p = at(e); line(ctx, last.x, last.y, p.x, p.y, pen); last = p; });
    var end = function () { last = null; };
    canvas.addEventListener('pointerup', end); canvas.addEventListener('pointercancel', end);
    form.addEventListener('submit', function (e) {
      if (kind.value !== 'painting') { painting.value = ''; return; }
      var data = ctx.getImageData(0, 0, W, H).data, ink = new Uint8Array(W * H), any = false;
      for (var i = 0; i < W * H; i++) { ink[i] = data[i * 4] < 128 ? 1 : 0; if (ink[i]) { any = true; } }
      if (!any) { e.preventDefault(); empty.hidden = false; return; }
      painting.value = b64(bmp1(ink));
    });
  });
})();
