import { escapeHtml, formatMoney, isBusiness } from '../config.js';

function edgeForIndex(index) {
  if (index <= 5) return 'top';
  if (index <= 10) return 'right';
  if (index <= 15) return 'bottom';
  return 'left';
}

export function renderBoard(board, deck, state, onSelect) {
  board.innerHTML = deck.map((tile, index) => {
    const edge = edgeForIndex(index);
    if (!isBusiness(tile)) {
      return `<button class="property property--special property--${tile.type} edge-${edge}" data-index="${index}"><span class="special-icon"><img src="${escapeHtml(tile.icon)}" alt="" /></span><h2>${tile.name}</h2></button>`;
    }
    const level = state.levels[index] || 1;
    const stars = [1,2,3,4,5].map((star) => `<i class="level-star ${star <= level ? 'is-filled' : ''}">★</i>`).join('');
    return `<button class="property edge-${edge} ${index === state.selected ? 'is-selected' : ''} ${tile.owner ? 'is-owned' : ''}" style="--property-color:${tile.color};--owner-color:${tile.ownerColor || 'transparent'}" data-index="${index}"><img class="property__photo" src="${escapeHtml(tile.photos?.[0] || '')}" alt="${escapeHtml(tile.name)}" /><span class="property__stripe"></span><h2>${escapeHtml(tile.name)}</h2><span class="property__price">${formatMoney(tile.purchasePrice)}</span><span class="property__level">${stars}</span></button>`;
  }).join('');
  board.querySelectorAll('.property').forEach((card) => {
    card.addEventListener('click', () => onSelect(Number(card.dataset.index)));
  });
  board.querySelectorAll('.property__photo').forEach((image) => {
    const index = Number(image.closest('.property').dataset.index);
    const photos = deck[index]?.photos || [];
    let photoIndex = 0;
    image.addEventListener('error', () => {
      photoIndex += 1;
      if (photoIndex < photos.length) {
        image.src = photos[photoIndex];
      } else {
        image.remove();
      }
    });
  });
}
