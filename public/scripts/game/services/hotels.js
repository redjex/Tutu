import { colors, getDates, hotelKey, specialTiles } from '../config.js';

const countryCatalog = {
  RU:{ name:'Россия', cities:['Москва','Санкт-Петербург','Казань','Сочи'] },
  TR:{ name:'Турция', cities:['Стамбул','Анталья','Аланья','Измир'] },
  TH:{ name:'Таиланд', cities:['Бангкок','Пхукет','Паттайя','Чиангмай'] },
  AE:{ name:'ОАЭ', cities:['Дубай','Абу-Даби','Шарджа','Рас-эль-Хайма'] },
  IT:{ name:'Италия', cities:['Рим','Милан','Флоренция','Венеция'] },
};

const requestedCountry = new URLSearchParams(window.location.search).get('country') || 'RU';
const selectedCountry = countryCatalog[requestedCountry] || countryCatalog.RU;
const starLevels = [1,2,3,4,5];

async function searchHotels(city, stars, dates) {
  const params = new URLSearchParams({ city, check_in:dates.checkIn, check_out:dates.checkOut, stars:String(stars) });
  const response = await fetch(`/api/hotels?${params}`);
  const data = await response.json();
  if (!response.ok) throw new Error(data.error || `Ошибка MCP для ${city}, ${stars}★`);

  const unique = new Map();
  (data.hotels || [])
    .filter((hotel) => Number(hotel.stars) === stars && hotel.name?.trim() && hotel.photos?.[0])
    .forEach((hotel) => unique.set(hotelKey(hotel), { ...hotel, city }));
  return { stars, hotels:[...unique.values()] };
}

export async function loadHotelPools() {
  const dates = getDates();
  const searches = starLevels.flatMap((stars) => selectedCountry.cities.map((city) => searchHotels(city, stars, dates)));
  const settled = await Promise.allSettled(searches);
  const results = settled.filter((result) => result.status === 'fulfilled').map((result) => result.value);

  const pools = Object.fromEntries(starLevels.map((stars) => {
    const unique = new Map();
    results
      .filter((result) => result.stars === stars)
      .flatMap((result) => result.hotels)
      .forEach((hotel) => unique.set(hotelKey(hotel), hotel));
    return [stars,[...unique.values()].sort(() => Math.random() - .5)];
  }));

  if (pools[1].length < 12) {
    throw new Error(`${selectedCountry.name}: найдено только ${pools[1].length} уникальных мест категории 1★, нужно 12`);
  }
  return pools;
}

function sourceForLevel(pools, level, seed, excludedKey = '') {
  const pool = pools[level] || [];
  if (!pool.length) return null;
  const offset = Math.abs(seed) % pool.length;
  return pool.find((hotel, index) => index >= offset && hotelKey(hotel) !== excludedKey)
    || pool.find((hotel) => hotelKey(hotel) !== excludedKey)
    || pool[offset];
}

function applyHotel(tile, source) {
  if (!source || Number(source.stars) !== tile.level) return false;
  Object.assign(tile, {
    sourceKey:hotelKey(source), name:source.name.trim(), price:source.best_offer?.price?.amount || 0,
    stars:Number(source.stars), rating:source.rating, city:source.city || '', photos:source.photos || [],
    checkout_url:source.checkout_url,
  });
  return true;
}

export function buildDeck(pools) {
  let hotelIndex = 0;
  const offset = Math.floor(Math.random() * pools[1].length);
  return Array.from({ length:20 }, (_, index) => {
    if (specialTiles[index]) return { ...specialTiles[index] };
    const seed = offset + hotelIndex++;
    const group = (hotelIndex - 1) % colors.length;
    const tile = { type:'hotel', level:1, seed, group, color:colors[group], purchasePrice:1000 + index * 350, visits:0, owner:null };
    applyHotel(tile, sourceForLevel(pools,1,seed));
    return tile;
  });
}

export function upgradeHotel(tile, pools, level) {
  const source = sourceForLevel(pools,level,tile.seed + level,tile.sourceKey);
  if (!source) return false;
  tile.level = level;
  return applyHotel(tile,source);
}
