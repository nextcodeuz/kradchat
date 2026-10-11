// KRADCHAT WASM boot — external file (CSP script-src 'self' inline skriptlarni bloklaydi).
// ?v= versiya parametri eski keshlangan app.wasm yuklanishining oldini oladi.
// Yangi build chiqqanda shu raqamni oshiring.
// Progress bilan yuklash: katta wasm (5MB) da foydalanuvchi qotib qolgan
// ekranni emas, jonli progress ko'radi. instantiateStreaming o'rniga
// qo'lda fetch+progress+instantiate (streaming progress bermaydi).
(function () {
  var go = new Go();
  var bar = document.querySelector('#wasm-bar');
  var pct = document.querySelector('#wasm-pct');
  var note = document.querySelector('#wasm-note');

  function progress(loaded, total) {
    var p = total > 0 ? Math.min(99, Math.floor((loaded / total) * 100)) : 0;
    if (bar) bar.style.width = p + '%';
    if (pct) pct.textContent = p + '%';
  }

  function fail(err) {
    var app = document.querySelector('#app');
    app.textContent = '';
    var wrap = document.createElement('div');
    wrap.className = 'auth-wrap';
    var card = document.createElement('div');
    card.className = 'auth-card glass';
    var h1 = document.createElement('h1');
    h1.className = 'auth-title';
    h1.textContent = 'KRADCHAT';
    var p = document.createElement('p');
    p.className = 'auth-subtitle';
    p.style.color = '#F87171';
    p.textContent = "WASM yuklab bo'lmadi: " + err;
    var btn = document.createElement('button');
    btn.className = 'btn-primary';
    btn.style.marginTop = '14px';
    btn.textContent = 'Qayta urinish';
    btn.onclick = function () { location.reload(); };
    card.appendChild(h1);
    card.appendChild(p);
    card.appendChild(btn);
    wrap.appendChild(card);
    app.appendChild(wrap);
  }

  if (!window.WebAssembly) {
    fail('brauzer WebAssembly ni qo\'llamaydi');
    return;
  }

  var url = '/wasm/app.wasm?v=36';
  fetch(url).then(function (resp) {
    if (!resp.ok) throw new Error('HTTP ' + resp.status);
    // gzip bo'lsa Content-Length siqilgan hajm — baribir monoton o'sadi.
    var total = parseInt(resp.headers.get('Content-Length') || '0', 10);
    if (!resp.body || !resp.body.getReader) return resp.arrayBuffer();
    var reader = resp.body.getReader();
    var chunks = [];
    var loaded = 0;
    function pump() {
      return reader.read().then(function (r) {
        if (r.done) {
          var len = 0;
          chunks.forEach(function (c) { len += c.length; });
          var buf = new Uint8Array(len);
          var off = 0;
          chunks.forEach(function (c) { buf.set(c, off); off += c.length; });
          return buf.buffer;
        }
        chunks.push(r.value);
        loaded += r.value.length;
        progress(loaded, total);
        return pump();
      });
    }
    return pump();
  }).then(function (buf) {
    if (bar) bar.style.width = '100%';
    if (pct) pct.textContent = '100%';
    if (note) note.textContent = 'Ishga tushirilmoqda...';
    return WebAssembly.instantiate(buf, go.importObject);
  }).then(function (result) {
    go.run(result.instance);
  }).catch(fail);
})();
