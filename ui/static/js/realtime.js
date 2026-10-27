(function () {
  'use strict';

  var userID = Number(document.body.dataset.user);
  if (!userID) return; // гость — чат недоступен

  var PAGE_SIZE = 10;
  var THROTTLE_MS = 200; // не злоупотребляем событием scroll
  var SCROLL_EDGE = 50;  // порог близости к началу списка, px

  var list = document.getElementById('msg-list');
  var withID = list ? Number(list.dataset.with) : 0;
  var oldest = list ? Number(list.dataset.oldest) || 0 : 0;
  var hasMore = list ? list.dataset.hasMore === 'true' : false;
  var loading = false;
  var lastScrollRun = 0;
  var ws = null;
  var closedByPage = false;

  function pad(n) {
    return String(n).padStart(2, '0');
  }

  function formatTime(iso) {
    var d = new Date(iso);
    if (isNaN(d.getTime())) return iso;
    return pad(d.getDate()) + '.' + pad(d.getMonth() + 1) + '.' + d.getFullYear() +
      ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes());
  }

  // Пузырь строится через createElement + textContent,
  // чтобы содержимое сообщений не попало в DOM как HTML.
  function makeBubble(m) {
    var div = document.createElement('div');
    div.className = 'msg-bubble ' + (m.sender_id === userID ? 'msg-mine' : 'msg-theirs');

    var meta = document.createElement('div');
    meta.className = 'msg-meta';

    var name = document.createElement('strong');
    name.textContent = '@' + m.sender;
    meta.appendChild(name);

    var date = document.createElement('span');
    date.textContent = formatTime(m.created_at);
    meta.appendChild(date);

    var p = document.createElement('p');
    p.textContent = m.content;

    div.appendChild(meta);
    div.appendChild(p);
    return div;
  }

  // ===== Страница сообщений: прокрутка к последним =====
  if (list) {
    list.scrollTop = list.scrollHeight;
  }

  function appendMessage(m) {
    if (!list) return;
    list.appendChild(makeBubble(m));
    list.scrollTop = list.scrollHeight;
  }

  // ===== Пагинация: догрузка старых при прокрутке вверх =====
  function loadOlder() {
    if (!hasMore || loading || oldest === 0 || !list) return;
    loading = true;

    fetch('/messages?with=' + withID + '&before=' + oldest + '&limit=' + PAGE_SIZE)
      .then(function (res) {
        if (!res.ok) throw new Error('HTTP ' + res.status);
        return res.json();
      })
      .then(function (messages) {
        if (!messages.length) {
          hasMore = false;
          return;
        }

        var prevHeight = list.scrollHeight;
        var fragment = document.createDocumentFragment();
        messages.forEach(function (m) {
          oldest = Math.min(oldest, m.id);
          fragment.appendChild(makeBubble(m));
        });
        list.insertBefore(fragment, list.firstChild);

        list.scrollTop = list.scrollHeight - prevHeight;
        hasMore = messages.length === PAGE_SIZE;
      })
      .catch(function () {
        hasMore = false;
      })
      .finally(function () {
        loading = false;
      });
  }

  function onScroll() {
    var now = Date.now();
    if (now - lastScrollRun < THROTTLE_MS) return; // throttle
    lastScrollRun = now;
    if (list && list.scrollTop <= SCROLL_EDGE) {
      loadOlder();
    }
  }

  if (list) {
    list.addEventListener('scroll', onScroll, { passive: true });
  }

  // ===== Панель «Кто онлайн»: превью и перестановка контакта наверх =====
  function refreshContact(m) {
    var link = document.querySelector('.conv-item[href="/messages?with=' + m.sender_id + '"]');
    if (!link) return;
    var body = link.querySelector('.contact-body');
    if (!body) return;

    var preview = body.querySelector('.msg-preview');
    if (preview) {
      preview.textContent = m.content;
    } else {
      var pv = document.createElement('span');
      pv.className = 'msg-preview';
      pv.textContent = m.content;
      body.appendChild(pv);
    }

    var container = link.parentNode;
    if (container && container.firstChild !== link) {
      container.insertBefore(link, container.firstChild);
    }
  }

  // ===== Тост-уведомление о новом сообщении =====
  function showToast(m) {
    var box = document.getElementById('chat-toasts');
    if (!box) return;

    var a = document.createElement('a');
    a.href = '/messages?with=' + m.sender_id;
    a.className = 'chat-toast';

    var title = document.createElement('strong');
    title.textContent = '✉️ Новое сообщение от @' + m.sender;
    var text = document.createElement('p');
    text.textContent = m.content;

    a.appendChild(title);
    a.appendChild(text);
    box.appendChild(a);

    setTimeout(function () {
      a.classList.add('hide');
      setTimeout(function () { a.remove(); }, 300);
    }, 5000);

    while (box.children.length > 3) {
      box.removeChild(box.firstChild);
    }
  }

  // ===== Новое личное сообщение =====
  function onMessage(m) {
    if (list && withID === m.sender_id) {
      appendMessage(m);
    }
    refreshContact(m);
    showToast(m);
  }

  // ===== Счётчики лайков/дизлайков =====
  // Событие приходит всем подключённым; обновляем только те формы реакций,
  // которые есть на текущей странице (см. data-post-id / data-comment-id в шаблонах).
  function onReaction(rc) {
    if (rc.target !== 'post' && rc.target !== 'comment') return;
    var forms = document.querySelectorAll('.reaction-form[data-' + rc.target + '-id="' + Number(rc.id) + '"]');
    forms.forEach(function (form) {
      var likes = form.querySelector('[data-count="likes"]');
      var dislikes = form.querySelector('[data-count="dislikes"]');
      if (likes) likes.textContent = rc.likes;
      if (dislikes) dislikes.textContent = rc.dislikes;
    });
  }

  // ===== WebSocket: приём событий без перезагрузки =====
  function onEvent(event) {
    if (!event) return;
    switch (event.type) {
      case 'message':
        if (event.message) onMessage(event.message);
        break;
      case 'reaction':
        if (event.reaction) onReaction(event.reaction);
        break;
    }
  }

  function connect() {
    var scheme = location.protocol === 'https:' ? 'wss://' : 'ws://';
    ws = new WebSocket(scheme + location.host + '/ws');

    ws.onmessage = function (evt) {
      try {
        onEvent(JSON.parse(evt.data));
      } catch (e) {
        /* игнорируем повреждённые события */
      }
    };

    ws.onclose = function () {
      if (closedByPage) return;
      setTimeout(connect, 3000); // переподключение после обрыва
    };

    ws.onerror = function () {
      try { ws.close(); } catch (e) { /* noop */ }
    };
  }

  window.addEventListener('beforeunload', function () {
    closedByPage = true;
    try { ws.close(); } catch (e) { /* noop */ }
  });

  connect();
})();