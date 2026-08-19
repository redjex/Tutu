const flagFiles = { RUSSIA:'ru', TURKEY:'tr', THAILAND:'th', UAE:'ae', ITALY:'it', RU:'ru', TR:'tr', TH:'th', AE:'ae', IT:'it' };
const list = document.querySelector('#lobbies-list');
const count = document.querySelector('#lobbies-count');
const profileEntry = document.querySelector('#profile-entry');
const createBackdrop = document.querySelector('#create-backdrop');
const loadingBackdrop = document.querySelector('#loading-backdrop');
const createForm = document.querySelector('#create-lobby-form');
const createSubmit = createForm.querySelector('.create-dialog__submit');
const countryFilter = document.querySelector('.filter-group');
let currentUser = null;
let rooms = [];
let countryOptions = [];
let countryMap = new Map();
let countryPage = 0;
const countriesPerPage = 3;
const countryPrev = document.querySelector('#country-prev');
const countryNext = document.querySelector('#country-next');
const countryPageLabel = document.querySelector('#country-page');

const escapeHtml = (value) => String(value ?? '').replace(/[&<>'"]/g, (char) => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', "'":'&#39;', '"':'&quot;' }[char]));

async function request(url, options) {
  const response = await fetch(url, options);
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || 'Сервер временно недоступен');
  return data;
}

function setCreateOpen(open) {
  createBackdrop.classList.toggle('is-open', open);
  createBackdrop.setAttribute('aria-hidden', String(!open));
  if (!open) document.querySelectorAll('[data-custom-select]').forEach((select) => {
    select.classList.remove('is-open');
    select.querySelector('.custom-select__trigger').setAttribute('aria-expanded', 'false');
  });
}

function setLoadingOpen(open) {
  loadingBackdrop.classList.toggle('is-hidden', !open);
  loadingBackdrop.setAttribute('aria-hidden', String(!open));
}

function updateProfileEntry() {
  const avatar = profileEntry.querySelector('.profile-entry__avatar');
  avatar.replaceChildren();
  if (currentUser?.picture) {
    const image = document.createElement('img');
    image.src = currentUser.picture;
    image.alt = '';
    image.referrerPolicy = 'no-referrer';
    avatar.append(image);
  }
  avatar.classList.toggle('oim-door-arrow-right', !currentUser);
  avatar.classList.toggle('is-profile-avatar', Boolean(currentUser));
  avatar.classList.toggle('has-image', Boolean(currentUser?.picture));
  profileEntry.querySelector('strong').textContent = currentUser?.name || 'Войти';
}

function beginGoogleOAuth(action = '', next = '/?oauth=success') {
  if (action) sessionStorage.setItem('oauth_action', action);
  location.href = `/api/auth/google/start?next=${encodeURIComponent(next)}`;
}

function activeFilters() {
  return {
    country:countryFilter.querySelector('[name="country"]:checked')?.value || 'all',
    modes:new Set([...document.querySelectorAll('.filters [name="mode"]:checked')].map((input) => input.value)),
  };
}

function roomLabel(value) {
  const last = Math.abs(value) % 100;
  const digit = last % 10;
  if (last > 10 && last < 20) return 'комнат';
  if (digit === 1) return 'комната';
  if (digit > 1 && digit < 5) return 'комнаты';
  return 'комнат';
}

function countryFlag(code) {
  const file = flagFiles[code];
  return file ? `/assets/images/flags/${file}.svg` : (code ? `https://flagcdn.com/w40/${String(code).toLowerCase()}.png` : '');
}

function syncCountryFilters() {
  const selected = countryFilter.querySelector('[name="country"]:checked')?.value || 'all';
  countryFilter.querySelectorAll('label').forEach((label, index) => {
    if (index > 0) label.remove();
  });
  const codes = [...new Set(rooms.map((room) => room.countryCode))].sort((left, right) => {
    return (countryMap.get(left)?.name || left).localeCompare(countryMap.get(right)?.name || right, 'ru');
  });
  codes.forEach((code) => {
    const country = countryMap.get(code) || { code, name:code };
    const label = document.createElement('label');
    const input = document.createElement('input');
    input.type = 'radio';
    input.name = 'country';
    input.value = code;
    input.checked = selected === code;
    const span = document.createElement('span');
    const flag = countryFlag(code);
    if (flag) {
      const image = document.createElement('img');
      image.src = flag;
      image.alt = '';
      span.append(image);
    }
    span.append(country.name);
    label.append(input, span);
    input.addEventListener('change', renderRooms);
    countryFilter.append(label);
  });
  if (selected !== 'all' && !codes.includes(selected)) countryFilter.querySelector('[value="all"]').checked = true;
  countryPage = Math.min(countryPage, Math.max(0, Math.ceil(codes.length / countriesPerPage) - 1));
  updateCountryPage(codes.length);
}

function updateCountryPage(total = countryFilter.querySelectorAll('label').length - 1) {
  const labels = [...countryFilter.querySelectorAll('label')].slice(1);
  const pages = Math.max(1, Math.ceil(total / countriesPerPage));
  labels.forEach((label, index) => { label.hidden = index < countryPage * countriesPerPage || index >= (countryPage + 1) * countriesPerPage; });
  countryPageLabel.textContent = `${countryPage + 1} / ${pages}`;
  countryPrev.disabled = countryPage <= 0;
  countryNext.disabled = countryPage >= pages - 1;
}

function renderRooms() {
  const filters = activeFilters();
  const visible = rooms
    .filter((room) => (filters.country === 'all' || room.countryCode === filters.country) && filters.modes.has(room.mode || 'classic'))
    .sort((first, second) => first.players - second.players || first.id.localeCompare(second.id));
  count.textContent = `${visible.length} ${roomLabel(visible.length)}`;
  if (!visible.length) {
    list.innerHTML = '<div class="empty-result">По выбранным фильтрам открытых лобби нет</div>';
    return;
  }
  list.innerHTML = visible.map((room) => {
    const country = countryMap.get(room.countryCode) || { name:room.countryName };
    const flag = countryFlag(room.countryCode);
    const image = room.coverUrl ? `<img src="${escapeHtml(room.coverUrl)}" alt="" loading="lazy" referrerpolicy="no-referrer" />` : '';
    return `<article class="lobby-card"><div class="lobby-card__image">${image}</div><div class="lobby-card__body"><span class="lobby-card__country">${flag ? `<img src="${flag}" alt="" />` : ''}${escapeHtml(country.name)}</span><h2>Комната #${escapeHtml(room.id)}</h2><div class="lobby-card__details"><span>${room.mode === 'fast' ? 'Быстрый режим' : 'Классический режим'}</span><span>${room.players} из ${room.capacity} игроков</span></div></div><button class="join-button" type="button" data-room-id="${escapeHtml(room.id)}">Войти</button></article>`;
  }).join('');
  list.querySelectorAll('.lobby-card__image img').forEach((image) => image.addEventListener('error', () => image.remove()));
}

async function loadCountries() {
  const data = await request('/api/countries');
  const displayNames = typeof Intl.DisplayNames === 'function' ? new Intl.DisplayNames(['ru'], { type:'region' }) : null;
  countryOptions = (data.countries || []).map((country) => ({
    ...country,
    name:displayNames?.of(country.code) || country.name,
  }));
  countryMap = new Map(countryOptions.map((country) => [country.code, country]));
  const select = createForm.querySelector('[data-custom-select]');
  select.classList.add('country-select');
  const value = select.querySelector('input[type="hidden"]');
  const trigger = select.querySelector('.custom-select__trigger span');
  const options = select.querySelector('.custom-select__options');
  const preferred = countryMap.get('RUSSIA') || countryMap.get('RU') || countryOptions[0];
  if (preferred) {
    value.value = preferred.code;
    trigger.textContent = preferred.name;
  }
  options.replaceChildren(...countryOptions.map((country) => {
    const button = document.createElement('button');
    button.type = 'button';
    button.dataset.value = country.code;
    const flag = countryFlag(country.code);
    if (flag) {
      const image = document.createElement('img');
      image.src = flag;
      image.alt = '';
      button.append(image);
    }
    button.append(country.name);
    return button;
  }));
  setupCountrySelectPagination(select);
}

function setupCountrySelectPagination(select) {
  const options = select.querySelector('.custom-select__options');
  const pagination = select.querySelector('.custom-select__pagination');
  const prev = pagination?.querySelector('[data-country-prev]');
  const next = pagination?.querySelector('[data-country-next]');
  const pageLabel = pagination?.querySelector('[data-country-page]');
  if (!pagination || !prev || !next || !pageLabel) return;
  let page = 0;
  const update = () => {
    const buttons = [...options.querySelectorAll('button')];
    const pages = Math.max(1, Math.ceil(buttons.length / countriesPerPage));
    page = Math.min(page, pages - 1);
    buttons.forEach((button, index) => {
      button.hidden = index < page * countriesPerPage || index >= (page + 1) * countriesPerPage;
    });
    pageLabel.textContent = `${page + 1} / ${pages}`;
    prev.disabled = page === 0;
    next.disabled = page >= pages - 1;
    pagination.hidden = pages <= 1;
  };
  prev.addEventListener('click', () => { page -= 1; update(); });
  next.addEventListener('click', () => { page += 1; update(); });
  update();
}

async function loadRooms() {
  const data = await request('/api/rooms');
  rooms = data.rooms || [];
  syncCountryFilters();
  renderRooms();
}

async function joinRoom(id) {
  if (!currentUser) {
    beginGoogleOAuth('', `/game?room=${encodeURIComponent(id)}`);
    return;
  }
  await request(`/api/rooms/${encodeURIComponent(id)}/join`, { method:'POST' });
  location.href = `/game?room=${encodeURIComponent(id)}`;
}

document.querySelectorAll('.filters [name="mode"]').forEach((input) => input.addEventListener('change', renderRooms));
countryFilter.querySelector('[value="all"]').addEventListener('change', renderRooms);
countryPrev.addEventListener('click', () => { countryPage -= 1; updateCountryPage(); });
countryNext.addEventListener('click', () => { countryPage += 1; updateCountryPage(); });
document.querySelector('#reset-filters').addEventListener('click', () => {
  countryFilter.querySelector('[value="all"]').checked = true;
  document.querySelectorAll('.filters [name="mode"]').forEach((input) => { input.checked = true; });
  renderRooms();
});

list.addEventListener('click', (event) => {
  const button = event.target.closest('.join-button');
  if (!button) return;
  button.disabled = true;
  joinRoom(button.dataset.roomId).catch((error) => {
    button.disabled = false;
    window.alert(error.message);
  });
});

profileEntry.addEventListener('click', async () => {
  if (!currentUser) {
    beginGoogleOAuth();
    return;
  }
  if (!window.confirm(`${currentUser.name}\n\nВыйти из аккаунта?`)) return;
  await fetch('/api/auth/logout', { method:'POST' });
  currentUser = null;
  updateProfileEntry();
});

document.querySelector('#create-lobby-button').addEventListener('click', () => {
  if (!currentUser) {
    beginGoogleOAuth('create');
    return;
  }
  setCreateOpen(true);
});

document.querySelector('#create-cancel').addEventListener('click', () => setCreateOpen(false));
createBackdrop.addEventListener('click', (event) => {
  if (event.target === createBackdrop) setCreateOpen(false);
});

function initializeCustomSelects() {
  document.querySelectorAll('[data-custom-select]').forEach((select) => {
    const trigger = select.querySelector('.custom-select__trigger');
    const value = select.querySelector('input[type="hidden"]');
    const options = [...select.querySelectorAll('.custom-select__options button')];
    options.forEach((option) => option.classList.toggle('is-current', option.dataset.value === value.value));
    trigger.addEventListener('click', () => {
      const willOpen = !select.classList.contains('is-open');
      document.querySelectorAll('[data-custom-select]').forEach((item) => {
        item.classList.remove('is-open');
        item.querySelector('.custom-select__trigger').setAttribute('aria-expanded', 'false');
      });
      select.classList.toggle('is-open', willOpen);
      trigger.setAttribute('aria-expanded', String(willOpen));
    });
    options.forEach((option) => option.addEventListener('click', () => {
      value.value = option.dataset.value;
      trigger.querySelector('span').textContent = option.textContent;
      options.forEach((item) => item.classList.toggle('is-current', item === option));
      select.classList.remove('is-open');
      trigger.setAttribute('aria-expanded', 'false');
    }));
  });
}

createForm.addEventListener('submit', async (event) => {
  event.preventDefault();
  const values = new FormData(createForm);
  const original = createSubmit.innerHTML;
  createSubmit.disabled = true;
  setCreateOpen(false);
  setLoadingOpen(true);
  const loadingStarted = performance.now();
  try {
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    const data = await request('/api/rooms', {
      method:'POST',
      headers:{ 'content-type':'application/json' },
      body:JSON.stringify({
        countryCode:String(values.get('country') || 'RU'),
        mode:String(values.get('mode') || 'classic'),
        visibility:String(values.get('visibility') || 'public'),
        maxPlayers:Number(values.get('capacity') || 4),
      }),
    });
    const remaining = 1100 - (performance.now() - loadingStarted);
    if (remaining > 0) await new Promise((resolve) => setTimeout(resolve, remaining));
    sessionStorage.removeItem('oauth_action');
    location.href = `/game?room=${encodeURIComponent(data.room.id)}`;
  } catch (error) {
    setLoadingOpen(false);
    setCreateOpen(true);
    window.alert(error.message);
    createSubmit.disabled = false;
    createSubmit.innerHTML = original;
  }
});

document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape') setCreateOpen(false);
});

async function restoreSession() {
  const parameters = new URLSearchParams(location.search);
  const authError = parameters.get('auth_error');
  if (authError) window.alert(authError);
  const response = await fetch('/api/auth/me');
  if (response.ok) currentUser = (await response.json()).user;
  updateProfileEntry();
  const next = parameters.get('next');
  if (next?.startsWith('/game?room=')) {
    if (currentUser) location.href = next;
    else beginGoogleOAuth('', next);
    return;
  }
  if (currentUser && sessionStorage.getItem('oauth_action') === 'create') {
    sessionStorage.removeItem('oauth_action');
    setCreateOpen(true);
  }
}

await loadCountries().catch(() => {});
initializeCustomSelects();
await Promise.all([restoreSession(), loadRooms().catch(() => {
  list.innerHTML = '<div class="empty-result">Не удалось загрузить открытые лобби</div>';
})]);
setInterval(() => {
  if (!document.hidden) loadRooms().catch(() => {});
}, 5000);
