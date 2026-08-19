import asyncio
import json
import os
import re
import time
from contextlib import asynccontextmanager
from datetime import date, timedelta
from html.parser import HTMLParser
from typing import Any
from urllib.parse import urlparse

import httpx
from fastapi import FastAPI, HTTPException, Query
from pydantic import BaseModel


MCP_URL = os.getenv("TUTU_MCP_URL", "https://mcp.tutu.ru/mcp")
MCP_PROTOCOL_VERSION = "2025-03-26"
CACHE_TTL_SECONDS = int(os.getenv("HOTEL_CACHE_TTL_SECONDS", "900"))
COUNTRY_INDEX_URL = "https://hotel.tutu.ru/c_iceland/cities/"
MCP_CONCURRENCY = int(os.getenv("TUTU_MCP_CONCURRENCY", "5"))
MCP_RETRIES = int(os.getenv("TUTU_MCP_RETRIES", "1"))


class Hotel(BaseModel):
    id: str
    name: str
    stars: int
    rating: float | None = None
    review_count: int | None = None
    address: str | None = None
    city: str
    photos: list[str]
    checkout_url: str
    price_amount: float | None = None
    price_currency: str | None = None


class PoolsResponse(BaseModel):
    check_in: str
    check_out: str
    source: str
    pools: dict[str, list[Hotel]]


class TutuMCPClient:
    def __init__(self) -> None:
        self.http = httpx.AsyncClient(timeout=httpx.Timeout(25), follow_redirects=True)
        self.request_id = 0
        self.initialized = False
        self.lock = asyncio.Lock()

    async def close(self) -> None:
        await self.http.aclose()

    async def rpc(self, method: str, params: dict[str, Any] | None = None) -> dict[str, Any]:
        self.request_id += 1
        payload: dict[str, Any] = {"jsonrpc": "2.0", "id": self.request_id, "method": method}
        if params is not None:
            payload["params"] = params
        response = await self.http.post(
            MCP_URL,
            headers={
                "Accept": "application/json, text/event-stream",
                "Content-Type": "application/json",
                "MCP-Protocol-Version": MCP_PROTOCOL_VERSION,
            },
            json=payload,
        )
        response.raise_for_status()
        body = self.decode_response(response.text)
        if body.get("error"):
            raise RuntimeError(body["error"].get("message", "Tutu MCP error"))
        return body

    def decode_response(self, text: str) -> dict[str, Any]:
        value = text.strip()
        if value.startswith("{"):
            return json.loads(value)
        entries = [line[5:].strip() for line in value.splitlines() if line.startswith("data:")]
        if not entries:
            raise RuntimeError("Tutu MCP returned an unsupported response")
        return json.loads(entries[-1])

    async def initialize(self) -> None:
        if self.initialized:
            return
        async with self.lock:
            if self.initialized:
                return
            await self.rpc(
                "initialize",
                {
                    "protocolVersion": MCP_PROTOCOL_VERSION,
                    "capabilities": {},
                    "clientInfo": {"name": "tutu-monopoly", "version": "2.0.0"},
                },
            )
            await self.rpc("tools/call", {"name": "get_hotels_instructions", "arguments": {}})
            self.initialized = True

    async def call_tool(self, name: str, arguments: dict[str, Any]) -> dict[str, Any]:
        result = await self.rpc("tools/call", {"name": name, "arguments": arguments})
        envelope = result.get("result", {})
        content = next((item for item in envelope.get("content", []) if item.get("type") == "text"), None)
        if not content:
            raise RuntimeError(f"Tutu MCP tool {name} returned no text payload")
        if envelope.get("isError"):
            raise RuntimeError(content.get("text", f"Tutu MCP tool {name} failed"))
        return json.loads(content["text"])

    async def search_hotels(self, city: str, check_in: str, check_out: str, stars: int) -> list[Hotel]:
        last_error: Exception | None = None
        for attempt in range(MCP_RETRIES + 1):
            try:
                async with mcp_semaphore:
                    return await self.search_hotels_once(city, check_in, check_out, stars)
            except (httpx.HTTPError, RuntimeError, json.JSONDecodeError) as error:
                last_error = error
                if attempt < MCP_RETRIES:
                    await asyncio.sleep(0.6 * (attempt + 1))
        error_name = type(last_error).__name__ if last_error else "UnknownError"
        error_text = str(last_error).strip() if last_error else ""
        raise RuntimeError(f"Hotel search failed for {city}, {stars} stars: {error_name}{': ' + error_text if error_text else ''}")

    async def search_hotels_once(self, city: str, check_in: str, check_out: str, stars: int) -> list[Hotel]:
        await self.initialize()
        payload = await self.call_tool(
            "search_hotels",
            {
                "city_name": city,
                "check_in": check_in,
                "check_out": check_out,
                "adults": 1,
                "page": 1,
                "page_size": 30,
                "stars": [stars],
                "view": "full",
            },
        )
        hotels: list[Hotel] = []
        for item in payload.get("hotels", []):
            photo_values = [
                value
                for value in item.get("photos", [])
                if isinstance(value, str)
                and value.startswith("http")
                and urlparse(value).hostname != "cdn.bronevik.com"
            ]
            checkout_url = item.get("best_offer", {}).get("checkout_url") or item.get("checkout_url")
            actual_stars = item.get("stars")
            name = item.get("name")
            identifier = item.get("hotel_id") or item.get("hotel_geo_id") or item.get("tutu_offer_id")
            if actual_stars != stars or not identifier or not name or not photo_values or not checkout_url:
                continue
            price = item.get("best_offer", {}).get("price") or {}
            hotels.append(
                Hotel(
                    id=str(identifier),
                    name=str(name).strip(),
                    stars=stars,
                    rating=item.get("rating"),
                    review_count=item.get("review_count"),
                    address=item.get("address"),
                    city=city,
                    photos=photo_values,
                    checkout_url=str(checkout_url),
                    price_amount=price.get("amount"),
                    price_currency=price.get("currency"),
                )
            )
        return hotels


mcp_semaphore = asyncio.Semaphore(MCP_CONCURRENCY)
client = TutuMCPClient()
cache: dict[str, tuple[float, PoolsResponse]] = {}
cache_lock = asyncio.Lock()
directory_cache: tuple[float, list[dict[str, str]]] | None = None
country_cache: dict[str, tuple[float, dict[str, Any]]] = {}


class LinkParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.links: list[tuple[str, str]] = []
        self.href = ""
        self.text: list[str] = []

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        if tag == "a":
            self.href = dict(attrs).get("href") or ""
            self.text = []

    def handle_data(self, data: str) -> None:
        if self.href:
            self.text.append(data)

    def handle_endtag(self, tag: str) -> None:
        if tag == "a" and self.href:
            self.links.append((self.href, "".join(self.text).strip()))
            self.href = ""
            self.text = []


async def fetch_links(url: str) -> list[tuple[str, str]]:
    response = await client.http.get(url)
    response.raise_for_status()
    parser = LinkParser()
    parser.feed(response.text)
    return parser.links


async def country_directory() -> list[dict[str, str]]:
    global directory_cache
    now = time.monotonic()
    if directory_cache and directory_cache[0] > now:
        return directory_cache[1]
    response = await client.http.get(COUNTRY_INDEX_URL)
    response.raise_for_status()
    parser = LinkParser()
    parser.feed(response.text)
    iso_codes = dict(re.findall(r"https://hotel\.tutu\.ru/c_([^/]+)/\\\",\\\"isoCode\\\":\\\"([A-Z]{2})\\\"", response.text))
    values: dict[str, dict[str, str]] = {}
    for href, label in parser.links:
        match = re.fullmatch(r"/c_([^/]+)/?", urlparse(href).path)
        if not match or not label.startswith("Отели "):
            continue
        slug = match.group(1)
        code = iso_codes.get(slug)
        if not code:
            continue
        name = re.sub(r"^Отели\s+(?:в|во|на)\s+", "", label).strip()
        if name:
            values[code] = {"code": code, "name": name, "slug": slug}
    result = sorted(values.values(), key=lambda item: item["name"])
    directory_cache = (now + 86400, result)
    return result


async def resolve_country(code: str) -> dict[str, Any]:
    normalized = code.strip().upper()
    cached = country_cache.get(normalized)
    if cached and cached[0] > time.monotonic():
        return cached[1]
    countries = await country_directory()
    country = next((item for item in countries if item["code"] == normalized), None)
    if not country:
        raise HTTPException(status_code=404, detail="Country is not available in Tutu")
    slug = country["slug"]
    links = await fetch_links(f"https://hotel.tutu.ru/c_{slug}/cities/")
    cities: list[str] = []
    for href, _ in links:
        match = re.fullmatch(rf"/c_{re.escape(slug)}/([^/]+)/?", urlparse(href).path)
        if not match or match.group(1) == "cities":
            continue
        city = match.group(1).replace("-", " ")
        if city not in cities:
            cities.append(city)
        if len(cities) == 4:
            break
    if not cities:
        raise HTTPException(status_code=502, detail="Tutu returned no cities for this country")
    result = {"code": country["code"], "name": country["name"], "cities": cities}
    country_cache[normalized] = (time.monotonic() + 86400, result)
    return result


@asynccontextmanager
async def lifespan(_: FastAPI):
    yield
    await client.close()


app = FastAPI(title="Tutu Monopoly Data Service", version="2.0.0", lifespan=lifespan)


def validate_dates(check_in: str, check_out: str) -> tuple[date, date]:
    try:
        start = date.fromisoformat(check_in)
        end = date.fromisoformat(check_out)
    except ValueError as error:
        raise HTTPException(status_code=400, detail="Dates must use YYYY-MM-DD") from error
    if start < date.today() or end <= start or end - start > timedelta(days=30):
        raise HTTPException(status_code=400, detail="Invalid stay dates")
    return start, end


async def build_pools(check_in: str, check_out: str, cities: list[str]) -> PoolsResponse:
    tasks = [client.search_hotels(city, check_in, check_out, stars) for stars in range(1, 6) for city in cities]
    results = await asyncio.gather(*tasks, return_exceptions=True)
    pools: dict[str, list[Hotel]] = {str(level): [] for level in range(1, 6)}
    seen: dict[str, set[str]] = {str(level): set() for level in range(1, 6)}
    result_index = 0
    for stars in range(1, 6):
        for _ in cities:
            result = results[result_index]
            result_index += 1
            if isinstance(result, BaseException):
                continue
            for hotel in result:
                key = str(stars)
                if hotel.id not in seen[key]:
                    seen[key].add(hotel.id)
                    pools[key].append(hotel)
    if len(pools["1"]) < 12:
        failures = [str(result) for result in results if isinstance(result, BaseException)]
        suffix = f": {failures[0]}" if failures else ""
        raise HTTPException(status_code=502, detail=f"Tutu MCP returned only {len(pools['1'])} suitable one-star hotels{suffix}")
    missing = [level for level in range(2, 6) if not pools[str(level)]]
    if missing:
        raise HTTPException(status_code=502, detail=f"Tutu MCP returned no suitable hotels for levels: {missing}")
    return PoolsResponse(check_in=check_in, check_out=check_out, source="https://mcp.tutu.ru/mcp", pools=pools)


@app.get("/health")
async def health() -> dict[str, str]:
    return {"status": "ok", "source": MCP_URL}


@app.get("/countries")
async def countries() -> dict[str, list[dict[str, str]]]:
    try:
        return {"countries": await country_directory()}
    except httpx.HTTPError as error:
        raise HTTPException(status_code=502, detail=f"Tutu country directory unavailable: {error}") from error


@app.get("/countries/{code}")
async def country(code: str) -> dict[str, Any]:
    try:
        return await resolve_country(code)
    except HTTPException:
        raise
    except httpx.HTTPError as error:
        raise HTTPException(status_code=502, detail=f"Tutu country directory unavailable: {error}") from error


@app.get("/hotels/pools", response_model=PoolsResponse)
async def hotel_pools(
    check_in: str = Query(...),
    check_out: str = Query(...),
    city: list[str] = Query(default=["Москва", "Санкт-Петербург"]),
) -> PoolsResponse:
    validate_dates(check_in, check_out)
    cities = list(dict.fromkeys(value.strip() for value in city if value.strip()))
    if not cities or len(cities) > 4:
        raise HTTPException(status_code=400, detail="Provide between one and four cities")
    key = json.dumps([check_in, check_out, cities], ensure_ascii=False)
    cached = cache.get(key)
    if cached and cached[0] > time.monotonic():
        return cached[1]
    async with cache_lock:
        cached = cache.get(key)
        if cached and cached[0] > time.monotonic():
            return cached[1]
        try:
            result = await build_pools(check_in, check_out, cities)
        except HTTPException:
            raise
        except (httpx.HTTPError, RuntimeError, json.JSONDecodeError) as error:
            raise HTTPException(status_code=502, detail=f"Tutu MCP unavailable: {error}") from error
        cache[key] = (time.monotonic() + CACHE_TTL_SECONDS, result)
        return result
