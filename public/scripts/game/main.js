import { escapeHtml, formatMoney, isBusiness, JAIL_BAIL_COST, state } from './config.js';
import { renderBoard } from './ui/board-view.js';
import { Dice } from './services/dice.js';
import { playJailSound } from './services/audio.js';

const elements = {
  board:document.querySelector('#board'),
  money:document.querySelector('#rail-money'),
  message:document.querySelector('#dice-message'),
  roll:document.querySelector('#roll-button'),
  bail:document.querySelector('#bail-button'),
  upgrade:document.querySelector('#upgrade-button'),
  auctionActions:document.querySelector('#auction-actions'),
  auctionStatus:document.querySelector('#auction-status'),
  auctionBidButton:document.querySelector('#auction-bid-button'),
  auctionDeclineButton:document.querySelector('#auction-decline-button'),
  auctionStartButton:document.querySelector('#auction-start-button'),
  resultPanel:document.querySelector('#result-panel'),
  resultAnimation:document.querySelector('#result-animation'),
  resultTitle:document.querySelector('#result-title'),
  resultLeave:document.querySelector('#result-leave'),
  diceBox:document.querySelector('#dice-box'),
  turnActions:document.querySelector('#turn-actions'),
  toast:document.querySelector('#toast'),
  propertyInfo:document.querySelector('#property-info'),
  propertyInfoContent:document.querySelector('#property-info-content'),
  propertyInfoClose:document.querySelector('#property-info-close'),
  rules:document.querySelector('#rules-modal'),
  tokens:document.querySelector('#player-tokens'),
  kicker:document.querySelector('.turn-kicker'),
  players:[...document.querySelectorAll('.rail-player')],
};

const dice = new Dice(document.querySelector('#dice-box'));
let deck = [];
let hotelPools = {};
let socket = null;
let reconnectTimer = null;
let reconnectDelay = 500;
let snapshotVersion = -1;
let lastWaitingRoom = '';
let snapshotQueue = Promise.resolve();
let moveAnimationQueue = Promise.resolve();
let propertyPhotoIndex = 0;
let resultLottie = null;
let leavingRoom = false;

function showToast(message) {
  elements.toast.textContent = message;
  elements.toast.classList.add('is-visible');
  clearTimeout(showToast.timer);
  showToast.timer = setTimeout(() => elements.toast.classList.remove('is-visible'), 5000);
}

function currentPlayer() {
  return state.players?.find((player) => player.id === state.userId) || null;
}

function updateMoney() {
  elements.money.textContent = formatMoney(state.cash);
}

function updateTurnAvailability() {
  if (state.status === 'finished') {
    elements.diceBox.hidden = true;
    elements.message.hidden = true;
    elements.turnActions.hidden = true;
    elements.roll.hidden = true;
    elements.bail.hidden = true;
    elements.upgrade.hidden = true;
    elements.auctionActions.hidden = true;
    elements.kicker.textContent = 'Игра окончена';
    showResult();
    return;
  }
  elements.roll.hidden = false;
  elements.diceBox.hidden = false;
  elements.message.hidden = false;
  elements.turnActions.hidden = false;
  elements.resultPanel.hidden = true;
  const player = currentPlayer();
  const isYourTurn = state.turnPlayerId === state.userId;
  const isJailed = (player?.jailTurns || 0) > 0;
  const awaiting = state.awaitingActionId === state.userId;
  const isWaiting = state.status === 'waiting';
  const auctionActive = Boolean(state.auction);
  elements.roll.style.display = isWaiting ? 'none' : '';
  elements.roll.disabled = !deck.length || state.rolling || state.status !== 'active' || !isYourTurn || auctionActive;
  elements.roll.textContent = awaiting && state.awaitingActionType === 'buy' ? 'Выставить на аукцион' : (awaiting ? 'Завершить ход' : (isJailed ? 'Отбыть ход' : 'Бросить кубик'));
  elements.bail.hidden = isWaiting || !isJailed || !isYourTurn;
  elements.bail.style.display = isWaiting ? 'none' : '';
  elements.bail.disabled = state.rolling || state.cash < JAIL_BAIL_COST;
  if (auctionActive) {
    elements.bail.hidden = true;
    elements.bail.style.display = 'none';
  }
  elements.upgrade.hidden = true;
  elements.upgrade.style.display = isWaiting ? 'none' : '';
  elements.kicker.textContent = isWaiting ? 'Ожидание игроков' : auctionActive ? 'Аукцион' : (isYourTurn ? 'Ваш ход' : 'Ход соперника');
  updateAuctionUI();
}

async function readResultAnimation(path) {
  const response = await fetch(path);
  if (!response.ok) throw new Error('Result animation unavailable');
  const compressed = await response.arrayBuffer();
  const stream = new Blob([compressed]).stream().pipeThrough(new DecompressionStream('gzip'));
  return JSON.parse(await new Response(stream).text());
}

async function showResult() {
  if (state.resultShown) return;
  state.resultShown = true;
  const player = currentPlayer();
  const won = Boolean(player && !player.bankrupt);
  elements.resultPanel.hidden = false;
  elements.resultTitle.textContent = won ? 'Вы выиграли' : 'Вы проиграли';
  try {
    const path = won ? '/assets/tgs/win.tgs' : '/assets/tgs/Lose.tgs';
    const animationData = await readResultAnimation(path);
    if (window.lottie) {
      resultLottie?.destroy();
      resultLottie = window.lottie.loadAnimation({container:elements.resultAnimation, renderer:'svg', loop:true, autoplay:true, animationData});
    }
  } catch (error) {
    console.warn(error);
  }
}

function updateAuctionUI() {
  const auction = state.auction;
  const active = Boolean(auction) && state.status === 'active';
  elements.auctionActions.hidden = state.revealing || (!active && !(state.awaitingActionId === state.userId && state.awaitingActionType === 'buy'));
  elements.auctionStartButton.hidden = true;
  const declined = active && auction.declinedIds?.includes(state.userId);
  const isAuctionTurn = active && auction.currentBidderId === state.userId;
  elements.auctionBidButton.hidden = !isAuctionTurn || declined;
  elements.auctionDeclineButton.hidden = !isAuctionTurn || declined;
  if (!active) return;
  const remaining = Math.max(0, Math.ceil((new Date(auction.endsAt).getTime() - Date.now()) / 1000));
  elements.auctionStatus.textContent = declined ? 'Вы отказались от участия' : `${isAuctionTurn ? 'Ваш ход' : 'Ход соперника'} · ${formatMoney(auction.highestBid || 0)} · ${remaining} сек.`;
}

function drawBoard() {
  renderBoard(elements.board, deck, state, (index) => {
    state.inspectedIndex = index;
    propertyPhotoIndex = 0;
    renderPropertyInfo(index);
    if (isBusiness(deck[index])) openPropertyInfo();
  });
}

function openPropertyInfo() {
  elements.propertyInfo.classList.add('is-open');
  elements.propertyInfo.setAttribute('aria-hidden', 'false');
}

function closePropertyInfo() {
  elements.propertyInfo.classList.remove('is-open');
  elements.propertyInfo.setAttribute('aria-hidden', 'true');
}

function renderPropertyInfo(index = state.inspectedIndex ?? state.position) {
  const tile = deck[index];
  if (!tile || !isBusiness(tile)) {
    elements.propertyInfoContent.innerHTML = '<div class="property-info__empty">Выберите отель на поле, чтобы посмотреть подробности</div>';
    return;
  }
  const photos = (tile.photos || []).filter(Boolean);
  const photo = photos[propertyPhotoIndex % Math.max(photos.length, 1)] || '';
  const level = Math.max(1, Math.min(5, Number(tile.level) || 1));
  const fullName = String(tile.name || '').trim();
  const displayName = fullName.length > 15 ? `${fullName.slice(0, 15)}...` : fullName;
  const ratingBars = Array.from({ length: 5 }, (_, position) => `<i class="property-info__rating-bar ${position < level ? 'is-filled' : ''}"></i>`).join('');
  const photoMarkup = photo ? `<div class="property-info__photo-view"><img src="${escapeHtml(photo)}" alt="${escapeHtml(fullName)}" /><div class="property-info__photo-copy"><h2 class="property-info__title" id="property-info-title">${escapeHtml(displayName)}</h2><p class="property-info__subtitle">${escapeHtml(tile.city || '')}</p></div><div class="property-info__photo-overlay"><span class="property-info__rating" aria-label="Рейтинг отеля">${ratingBars}</span><strong>${formatMoney(tile.purchasePrice)}</strong></div>${photos.length > 1 ? '<button class="property-info__photo-prev" type="button" aria-label="Предыдущее фото">‹</button><button class="property-info__photo-next" type="button" aria-label="Следующее фото">›</button>' : ''}</div>` : `<h2 class="property-info__title" id="property-info-title">${escapeHtml(displayName)}</h2>`;
  const rentMarkup = [1, 2, 3, 4, 5].map((level) => `<span class="property-info__rent-stars" data-stars="${level}">${Array.from({ length: level }, () => '<img src="/assets/images/star.svg" alt="" />').join('')}</span><span>${formatMoney(Math.round(tile.purchasePrice * level / 5))}</span>`).join('');
  elements.propertyInfoContent.innerHTML = `${photoMarkup}<div class="property-info__section"><h3>Оплата за остановку</h3><div class="property-info__table">${rentMarkup}</div></div>`;
  elements.propertyInfoContent.querySelector('.property-info__photo-prev')?.addEventListener('click', () => { propertyPhotoIndex = (propertyPhotoIndex - 1 + photos.length) % photos.length; renderPropertyInfo(index); });
  elements.propertyInfoContent.querySelector('.property-info__photo-next')?.addEventListener('click', () => { propertyPhotoIndex = (propertyPhotoIndex + 1) % photos.length; renderPropertyInfo(index); });
}

function placePlayerToken(element, player, index, immediate = false) {
  const tileElement = elements.board.querySelector(`[data-index="${index}"]`);
  const tile = deck[index];
  if (!tileElement || !tile) return;
  const boardRect = elements.board.getBoundingClientRect();
  const shellRect = elements.board.parentElement.getBoundingClientRect();
  const tileRect = tileElement.getBoundingClientRect();
  const x = boardRect.left - shellRect.left + tileRect.left - boardRect.left + 7;
  const y = boardRect.top - shellRect.top + tileRect.top - boardRect.top + (isBusiness(tile) ? tileRect.height - 48 : 7);
  const offset = (player.slot || 0) * 5;
  if (immediate) element.style.transition = 'none';
  element.style.transform = `translate3d(${x + offset}px,${y + offset}px,0)`;
  element.classList.add('is-visible');
  if (immediate) requestAnimationFrame(() => {
    element.style.transition = '';
  });
}

function renderPlayerTokens() {
  const players = state.players || [];
  const existing = new Map([...elements.tokens.children].map((element) => [element.dataset.playerId, element]));
  players.forEach((player, index) => {
    let element = existing.get(player.id);
    if (!element) {
      element = document.createElement('span');
      element.className = 'player-token';
      element.dataset.playerId = player.id;
      element.title = player.name || '';
      elements.tokens.append(element);
    }
    player.slot = index;
    element.replaceChildren();
    if (player.picture) {
      const image = document.createElement('img');
      image.src = player.picture;
      image.alt = '';
      image.referrerPolicy = 'no-referrer';
      image.addEventListener('error', () => {
        element.textContent = player.name?.charAt(0).toUpperCase() || String(index + 1);
      }, {once:true});
      element.append(image);
    } else {
      element.textContent = player.name?.charAt(0).toUpperCase() || String(index + 1);
    }
    element.style.background = player.color || '';
    placePlayerToken(element, player, player.position || 0, true);
    existing.delete(player.id);
  });
  existing.forEach((element) => element.remove());
}

async function animatePlayerMove(playerId, fromPosition, steps) {
  const player = state.players?.find((item) => item.id === playerId);
  const element = elements.tokens.querySelector(`[data-player-id="${CSS.escape(playerId)}"]`);
  if (!player || !element) return;
  const movingPlayer = {...player, position:fromPosition};
  placePlayerToken(element, movingPlayer, fromPosition, true);
  for (let step = 0; step < steps; step += 1) {
    movingPlayer.position = (movingPlayer.position + 1) % deck.length;
    placePlayerToken(element, movingPlayer, movingPlayer.position);
    await new Promise((resolve) => setTimeout(resolve, 210));
  }
}

async function playMoveAnimation(playerId, fromPosition, steps) {
  try {
    await dice.roll(steps);
    await animatePlayerMove(playerId, fromPosition, steps);
  } catch (error) {
    console.warn('Move animation failed', error);
    renderPlayerTokens();
  }
}

function queueMoveAnimation(playerId, fromPosition, steps) {
  moveAnimationQueue = moveAnimationQueue
    .catch(() => {})
    .then(() => playMoveAnimation(playerId, fromPosition, steps));
  return moveAnimationQueue;
}

function ownsColorGroup(tile) {
  const group = deck.filter((item) => isBusiness(item) && item.group === tile.group);
  return group.length > 0 && group.every((item) => item.owner === state.userId);
}

function upgradeCost(tile, level) {
  return Math.round(tile.purchasePrice * (.3 + level * .1));
}

function updateUpgradeButton() {
  const tile = deck[state.position];
  const awaiting = state.awaitingActionId === state.userId;
  elements.upgrade.hidden = state.revealing || !awaiting || state.awaitingActionType === 'auction';
  if (!isBusiness(tile) || !awaiting) {
    elements.upgrade.disabled = true;
    elements.upgrade.textContent = isBusiness(tile) ? 'Дождитесь своего хода' : 'Встаньте на отель';
    return;
  }
  if (state.awaitingActionType === 'buy') {
    elements.upgrade.disabled = state.cash < tile.purchasePrice;
    elements.upgrade.textContent = `Купить за ${formatMoney(tile.purchasePrice)}`;
    return;
  }
  const level = tile.level || 1;
  const cost = upgradeCost(tile, level);
  elements.upgrade.disabled = !ownsColorGroup(tile) || level >= 5 || state.cash < cost;
  elements.upgrade.textContent = level >= 5 ? 'Максимальный уровень' : `Улучшить за ${formatMoney(cost)}`;
}

function applyTileEffect(tile) {
  if (state.status === 'waiting') {
    elements.message.textContent = `Подключено ${state.players?.length || 0} из ${state.settings?.maxPlayers || 0} игроков`;
    return;
  }
  elements.message.textContent = state.message || tile?.name || '';
}

function changeCash(outcomes, label) {
  const amount = outcomes[0] || 0;
  elements.message.textContent = `${label}: ${formatMoney(amount)}`;
}

function updatePlayers() {
  elements.players.forEach((element, index) => {
    const player = state.players?.[index];
    const avatar = element.querySelector('.rail-avatar');
    const name = element.querySelector('strong');
    const balance = element.querySelector('.rail-balance');
    if (!player) {
      element.hidden = true;
      return;
    }
    element.hidden = false;
    const lost = Boolean(player.bankrupt);
    const winner = state.status === 'finished' && !lost;
    element.classList.toggle('is-lost', lost);
    element.classList.toggle('is-winner', winner);
    element.dataset.status = lost ? 'Проиграл' : winner ? 'Победитель' : '';
    avatar.replaceChildren();
    if (player.picture) {
      const image = document.createElement('img');
      image.src = player.picture;
      image.alt = '';
      avatar.append(image);
    } else {
      avatar.textContent = player.name?.charAt(0).toUpperCase() || index + 1;
    }
    name.textContent = player.id === state.userId ? 'Вы' : player.name;
    balance.textContent = player.bankrupt ? 'Банкрот' : formatMoney(player.cash);
  });
}

async function applySnapshot(snapshot) {
  if (snapshot.version < snapshotVersion) return;
  const previousPlayers = state.players || [];
  const previousTurnPlayerId = state.turnPlayerId;
  const previous = previousPlayers.find((player) => player.id === state.userId) || null;
  const previousPosition = previous?.position ?? state.position;
  const previousJailTurns = previous?.jailTurns || 0;
  const wasInitial = snapshotVersion < 0;
  snapshotVersion = snapshot.version;
  state.roomId = snapshot.id;
  state.userId = snapshot.youId;
  state.turnPlayerId = snapshot.turnPlayerId;
  state.awaitingActionId = snapshot.awaitingActionId || '';
  state.awaitingActionType = snapshot.awaitingActionType || '';
  state.status = snapshot.status;
  state.message = snapshot.message;
  state.settings = snapshot.settings || null;
  state.auction = snapshot.auction || null;
  state.players = snapshot.players || [];
  deck = snapshot.deck || [];
  state.levels = deck.map((tile) => tile.level || 1);
  const player = currentPlayer();
  state.cash = player?.cash || 0;
  state.selected = isBusiness(deck[player?.position]) ? player.position : null;
  drawBoard();
  const previousActor = previousPlayers.find((item) => item.id === previousTurnPlayerId);
  const currentActor = state.players.find((item) => item.id === previousTurnPlayerId);
  const hasMove = !wasInitial && previousActor && currentActor && previousActor.position !== currentActor.position && snapshot.lastDice;
  renderPlayerTokens();
  let moveAnimation = Promise.resolve();
  state.revealing = Boolean(hasMove);
  if (hasMove) {
    const actorElement = elements.tokens.querySelector(`[data-player-id="${CSS.escape(previousTurnPlayerId)}"]`);
    if (actorElement) {
      placePlayerToken(actorElement, {...currentActor, position:previousActor.position}, previousActor.position, true);
    }
    state.position = previousPosition;
    // Анимация не должна блокировать применение следующих серверных состояний.
    moveAnimation = queueMoveAnimation(previousTurnPlayerId, previousActor.position, snapshot.lastDice);
  }
  moveAnimation.then(() => {
    state.revealing = false;
    updateTurnAvailability();
    updateUpgradeButton();
  });
  state.position = player?.position || 0;
  if (player?.jailTurns > previousJailTurns) playJailSound();
  state.rolling = false;
  updateMoney();
  updatePlayers();
  applyTileEffect(deck[state.position]);
  updateTurnAvailability();
  updateUpgradeButton();
  drawBoard();
  renderPropertyInfo();
  if (!wasInitial && snapshot.message) moveAnimation.then(() => showToast(snapshot.message));
  if (snapshot.status === 'waiting' && lastWaitingRoom !== snapshot.id) {
    lastWaitingRoom = snapshot.id;
    showToast(`${snapshot.settings?.name || 'Лобби'} · код ${snapshot.id}`);
  }
  if (snapshot.status === 'active' && lastWaitingRoom === snapshot.id) {
    lastWaitingRoom = '';
    showToast('Все игроки подключились. Партия начинается');
  }
}

function sendCommand(type, amount = 0) {
  if (!socket || socket.readyState !== WebSocket.OPEN) {
    showToast('Соединение восстанавливается');
    return false;
  }
  socket.send(JSON.stringify({ type, amount }));
  return true;
}

async function roll() {
  if (state.rolling) return;
  const type = state.awaitingActionId === state.userId
    ? (state.awaitingActionType === 'buy' ? 'auction_start' : 'end_turn')
    : 'roll';
  state.rolling = sendCommand(type);
  updateTurnAvailability();
  elements.message.textContent = type === 'roll' ? 'Кубик бросается…' : 'Завершаем ход…';
}

function handlePropertyAction() {
  if (state.rolling || state.awaitingActionId !== state.userId) return;
  state.rolling = sendCommand('property');
  updateTurnAvailability();
}

function startAuction() {
  if (state.rolling) return;
  state.rolling = sendCommand('auction_start');
  updateTurnAvailability();
}

function bidAuction() {
  if (!state.auction) return;
  const amount = (state.auction.highestBid || 0) + 100;
  state.rolling = sendCommand('auction_bid', amount);
}

function declineAuction() {
  if (!state.auction) return;
  state.rolling = sendCommand('auction_decline');
}

function payJailBail() {
  if (state.rolling || state.cash < JAIL_BAIL_COST) return;
  state.rolling = sendCommand('bail');
  updateTurnAvailability();
}

function connectSocket() {
  clearTimeout(reconnectTimer);
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  socket = new WebSocket(`${protocol}//${location.host}/api/rooms/${encodeURIComponent(state.roomId)}/ws`);
  socket.addEventListener('open', () => {
    reconnectDelay = 500;
  });
  socket.addEventListener('message', (event) => {
    const payload = JSON.parse(event.data);
    if (payload.type === 'state') snapshotQueue = snapshotQueue.then(() => applySnapshot(payload.state));
    if (payload.type === 'error') {
      state.rolling = false;
      updateTurnAvailability();
      showToast(payload.error);
    }
  });
  socket.addEventListener('close', () => {
    state.rolling = false;
    updateTurnAvailability();
    if (leavingRoom) return;
    reconnectTimer = setTimeout(connectSocket, reconnectDelay);
    reconnectDelay = Math.min(reconnectDelay * 2, 10000);
  });
}

async function start() {
  const roomId = new URLSearchParams(location.search).get('room')?.trim().toUpperCase();
  if (!roomId) {
    location.href = '/';
    return;
  }
  state.roomId = roomId;
  elements.roll.disabled = true;
  elements.board.innerHTML = '<div class="board-loading">Подключаемся к партии…</div>';
  try {
    const [response] = await Promise.all([
      fetch(`/api/rooms/${encodeURIComponent(roomId)}/join`, { method:'POST' }),
      dice.init(),
    ]);
    const data = await response.json();
    if (response.status === 401) {
      location.href = `/?next=${encodeURIComponent(`/game?room=${roomId}`)}`;
      return;
    }
    if (response.status === 409) {
      elements.board.innerHTML = '<div class="board-loading">Партия уже началась.<br><small>Новые игроки больше не допускаются.</small><br><button type="button" onclick="location.href=\'/\'">Вернуться в лобби</button></div>';
      elements.message.textContent = 'Вход в эту партию закрыт';
      return;
    }
    if (!response.ok) throw new Error(data.error || 'Не удалось подключиться к комнате');
    await applySnapshot(data.room);
    connectSocket();
  } catch (error) {
    elements.board.innerHTML = `<div class="board-loading board-loading--error">Не удалось подключиться к партии.<br><small>${escapeHtml(error.message)}</small><br><button type="button" onclick="location.reload()">Повторить</button></div>`;
    elements.message.textContent = 'Игра пока недоступна';
  }
}

elements.roll.addEventListener('click', roll);
elements.bail.addEventListener('click', payJailBail);
elements.upgrade.addEventListener('click', handlePropertyAction);
elements.auctionStartButton.addEventListener('click', startAuction);
elements.auctionBidButton.addEventListener('click', bidAuction);
elements.auctionDeclineButton.addEventListener('click', declineAuction);
elements.resultLeave.addEventListener('click', () => document.querySelector('.leave-button').click());
elements.propertyInfoClose.addEventListener('click', closePropertyInfo);
elements.propertyInfo.addEventListener('click', (event) => {
  if (event.target === elements.propertyInfo) closePropertyInfo();
});
document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape') closePropertyInfo();
});
document.querySelector('#rules-button').addEventListener('click', () => elements.rules.classList.add('is-open'));
document.querySelector('#rules-close').addEventListener('click', () => elements.rules.classList.remove('is-open'));
elements.rules.addEventListener('click', (event) => {
  if (event.target === elements.rules) elements.rules.classList.remove('is-open');
});
document.querySelector('#invite-button').addEventListener('click', async () => {
  const link = `${location.origin}/game?room=${encodeURIComponent(state.roomId)}`;
  try {
    await navigator.clipboard.writeText(link);
    showToast('Ссылка на лобби скопирована');
  } catch {
    window.prompt('Скопируйте ссылку на лобби', link);
  }
});
document.querySelector('.leave-button').addEventListener('click', async (event) => {
  if (leavingRoom) return;
  leavingRoom = true;
  event.currentTarget.disabled = true;
  clearTimeout(reconnectTimer);
  try {
    await fetch(`/api/rooms/${encodeURIComponent(state.roomId)}/leave`, { method:'POST', keepalive:true });
  } finally {
    socket?.close();
    location.href = '/';
  }
});
window.addEventListener('resize', () => renderPlayerTokens());
setInterval(updateAuctionUI, 250);

start();
