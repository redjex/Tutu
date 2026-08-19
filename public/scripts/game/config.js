export const state = {
  cash: 15000,
  position: 0,
  round: 1,
  selected: null,
  inspectedIndex: null,
  levels: [],
  jailTurns: 0,
  rolling: false,
  roomId: '',
  userId: '',
  turnPlayerId: '',
  awaitingActionId: '',
  awaitingActionType: '',
  status: 'loading',
  message: '',
  settings: null,
  auction: null,
  revealing: false,
  resultShown: false,
};

export const JAIL_BAIL_COST = 2000;

export const colors = ['#7889d7', '#78b99b', '#efad69', '#d98787', '#9b86cb', '#65a8be'];

export const specialTiles = {
  0: { type:'start', name:'Старт', icon:'/assets/images/fly.svg', note:'Получите $2 000' },
  3: { type:'chance', name:'Шанс', icon:'/assets/images/change.svg', note:'Случайное событие' },
  5: { type:'perk', name:'Плюшка', icon:'/assets/images/star.svg', note:'Получите бонус' },
  8: { type:'cafe', name:'Кафе', icon:'/assets/images/market.svg', note:'Перерыв и бонус $300' },
  10: { type:'jail', name:'Тюрьма', icon:'/assets/images/prizen.svg', note:'Пропуск двух ходов' },
  13: { type:'cafe', name:'Кафе', icon:'/assets/images/market.svg', note:'Перерыв и бонус $300' },
  15: { type:'tax', name:'Налог', icon:'/assets/images/fee.svg', note:'Заплатите $1 000' },
  18: { type:'chance', name:'Шанс', icon:'/assets/images/change.svg', note:'Случайное событие' },
};

export const diceFiles = [
  '/assets/dice/Dice_NewEmojis_008_AgAD6gIAAt.tgs',
  '/assets/dice/Dice_NewEmojis_009_AgADgAQAAn.tgs',
  '/assets/dice/Dice_NewEmojis_010_AgADpgMAAv.tgs',
  '/assets/dice/Dice_NewEmojis_011_AgADjQMAAg.tgs',
  '/assets/dice/Dice_NewEmojis_012_AgADLwMAAv.tgs',
  '/assets/dice/Dice_NewEmojis_013_AgADigMAAh.tgs',
];

export const isBusiness = (tile) => tile?.type === 'hotel';
export const hotelKey = (hotel) => hotel?.id || hotel?.checkout_url || `${hotel?.name}|${hotel?.photos?.[0] || ''}`;
export const formatMoney = (value) => `$ ${Number(value).toLocaleString('ru-RU')}`;
export const escapeHtml = (value) => String(value ?? '').replace(/[&<>'"]/g, (char) => ({ '&':'&amp;', '<':'&lt;', '>':'&gt;', "'":'&#39;', '"':'&quot;' }[char]));

export function getDates() {
  const checkIn = new Date();
  checkIn.setDate(checkIn.getDate() + 1);
  const checkOut = new Date(checkIn);
  checkOut.setDate(checkOut.getDate() + 1);
  const iso = (date) => date.toISOString().slice(0, 10);
  return { checkIn:iso(checkIn), checkOut:iso(checkOut) };
}
