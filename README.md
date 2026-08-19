# Tutu Monopoly

Многопользовательская браузерная игра с комнатами, Google OAuth, WebSocket-синхронизацией и данными отелей Tutu MCP.

## Production-запуск

```bash
cp .env.example .env
docker compose up -d --build
```

Основное приложение: `http://localhost:5510`.

В production рекомендуется запускать его за Nginx или Caddy с HTTPS. Для Google OAuth укажи в `.env`:

```env
PUBLIC_BASE_URL=https://example.com
COOKIE_SECURE=true
ALLOWED_ORIGINS=https://example.com
```

## Проверка

```bash
docker compose ps
curl http://127.0.0.1:5510/health
docker compose logs --tail=100 game
```

## Структура

- `cmd/server` — Go-сервер, комнаты, авторизация и WebSocket.
- `backend_python` — Python-сервис поиска отелей через Tutu MCP.
- `public` — frontend и статические ресурсы.
- `Dockerfile.python`, `Go.Dockerfile`, `docker-compose.yml` — production-сборка.

Сессии авторизации сохраняются в Docker volume `game-data`. Активные комнаты хранятся в памяти и сбрасываются при перезапуске приложения.
