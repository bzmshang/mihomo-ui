// Panel bootstrap, injected into the served index.html.
//
// The panel serves zashboard, which is a client for a Clash API and keeps the
// credential it is given in localStorage. Everything here is same-origin, so
// that credential is the panel's session token and the kernel secret never
// reaches the browser: the Go proxy swaps one for the other on the way out.
//
// This runs as a classic script before zashboard's deferred module, which
// matters — zashboard reads its backend list while that module evaluates, and a
// later write would not reach the already-created ref.
;(function () {
  var TOKEN_KEY = 'ui_token'
  var CLASH_PATH = '/clash'
  var BACKEND_ID = 'mihomo-ui'

  function readToken() {
    try {
      return localStorage.getItem(TOKEN_KEY) || ''
    } catch (e) {
      return ''
    }
  }

  function writeToken(token) {
    try {
      if (token) localStorage.setItem(TOKEN_KEY, token)
      else localStorage.removeItem(TOKEN_KEY)
    } catch (e) {
      /* private mode: the panel will simply ask again next load */
    }
  }

  // seedBackend publishes this deployment as zashboard's one backend.
  //
  // The two keys are written the way vueuse's useStorage serializes them, which
  // is decided by the default value's type: the list default is an array (JSON)
  // and the active-uuid default is a string (raw). Getting that wrong leaves the
  // panel sitting on zashboard's own setup screen.
  function seedBackend(token) {
    var backend = {
      type: 'clash',
      protocol: location.protocol.replace(':', ''),
      host: location.hostname,
      port: location.port || (location.protocol === 'https:' ? '443' : '80'),
      secondaryPath: CLASH_PATH,
      password: token,
      uuid: BACKEND_ID,
      label: 'mihomo',
      // Upgrading the kernel in place would replace the child process this panel
      // supervises and manages config.yaml for, so the button is not offered.
      disableUpgradeCore: true,
      // disableTunMode is deliberately left off: the TUN switch is how this panel
      // turns the tunnel on, and the proxy mirrors that patch into settings.yaml
      // so it survives the next apply.
    }
    try {
      localStorage.setItem('setup/api-list', JSON.stringify([backend]))
      localStorage.setItem('setup/active-uuid', BACKEND_ID)
    } catch (e) {
      /* nothing else to do: zashboard falls back to its setup page */
    }
  }

  // seedPanelSettings hides panel rows this deployment does not expose.
  //
  // zashboard persists per-row visibility in a map it reads at startup, so the
  // seed has to be the first writer or an existing panel would keep whatever it
  // remembered, and every later load has to see the same map or the customization
  // dialog would offer to switch a row back on.
  //
  // The ports grid is hidden because this panel does not offer port editing:
  // every row there writes a top-level key straight to the running kernel, and
  // the next apply merges override.yaml and the subscription back over it, so an
  // edit would look applied and then quietly revert.
  var SETTINGS_KEY = 'config/hidden-settings-items'
  var HIDDEN_SETTINGS = { 'backendSettings.ports': true }

  function seedPanelSettings() {
    try {
      localStorage.setItem(SETTINGS_KEY, JSON.stringify(HIDDEN_SETTINGS))
    } catch (e) {
      /* the row stays visible; the apply above still wins */
    }
  }

  // --- login overlay ------------------------------------------------------
  //
  // zashboard has no login screen and the panel has a password, so the form is
  // built here. It is shown over the app rather than on its own route: the
  // service worker answers every navigation with index.html, so a /login path
  // would never reach the server.

  var overlay = null

  function showLogin(message) {
    if (overlay) return
    overlay = document.createElement('div')
    overlay.id = 'panel-login'
    overlay.setAttribute(
      'style',
      [
        'position:fixed',
        'inset:0',
        'z-index:2147483647',
        'display:flex',
        'align-items:center',
        'justify-content:center',
        'background:#f5f6f7',
        'font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif',
        'color:#1f2329',
      ].join(';'),
    )

    var card = document.createElement('form')
    card.setAttribute(
      'style',
      [
        'width:320px',
        'max-width:90vw',
        'display:flex',
        'flex-direction:column',
        'gap:12px',
        'padding:24px',
        'border-radius:16px',
        'background:#fff',
        'box-shadow:0 4px 24px rgba(0,0,0,.06)',
      ].join(';'),
    )

    var title = document.createElement('h1')
    title.textContent = 'Mihomo'
    title.setAttribute('style', 'margin:0;font-size:18px;font-weight:600')
    card.appendChild(title)

    var input = document.createElement('input')
    input.type = 'password'
    input.autocomplete = 'current-password'
    input.placeholder = '请输入密码'
    input.setAttribute(
      'style',
      'height:38px;padding:0 12px;border:1px solid rgba(0,0,0,.12);border-radius:10px;font:inherit;outline:none',
    )
    card.appendChild(input)

    var error = document.createElement('p')
    error.textContent = message || ''
    error.setAttribute('style', 'margin:0;min-height:20px;color:#ff3b30;font-size:13px')
    card.appendChild(error)

    var button = document.createElement('button')
    button.type = 'submit'
    button.textContent = '登录'
    button.setAttribute(
      'style',
      'height:38px;border:0;border-radius:10px;background:#3482ff;color:#fff;font:inherit;font-weight:600;cursor:pointer',
    )
    card.appendChild(button)

    card.addEventListener('submit', function (event) {
      event.preventDefault()
      submitLogin(input.value, error, button)
    })

    overlay.appendChild(card)
    document.body.appendChild(overlay)
    input.focus()
  }

  function submitLogin(password, error, button) {
    if (!password) return
    button.disabled = true
    button.textContent = '登录中…'
    fetch('/api/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: password }),
    })
      .then(function (res) {
        return res.json().then(function (body) {
          return { ok: res.ok, body: body }
        })
      })
      .then(function (out) {
        if (!out.ok) {
          throw new Error((out.body && out.body.error) || '密码错误')
        }
        // auth:false means the panel runs without a password and issued no
        // token; an empty one is then the correct thing to seed.
        writeToken(out.body && out.body.token ? out.body.token : '')
        location.reload()
      })
      .catch(function (err) {
        error.textContent = err.message || '登录失败'
        button.disabled = false
        button.textContent = '登录'
        writeToken('')
        password = ''
      })
  }

  // --- boot ---------------------------------------------------------------

  var token = readToken()
  // Seeded unconditionally and synchronously: with no password configured the
  // empty token is correct, and the check below decides whether to cover the
  // app with the login form.
  seedBackend(token)
  seedPanelSettings()

  fetch('/api/auth/check', {
    headers: token ? { Authorization: 'Bearer ' + token } : {},
  })
    .then(function (res) {
      return res.json()
    })
    .then(function (body) {
      if (body && body.required && !body.ok) {
        writeToken('')
        showLogin('')
      }
    })
    .catch(function () {
      /* the panel is unreachable; zashboard will report it on its own */
    })
})()
