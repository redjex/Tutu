FROM golang:1.24-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -o /tutu-monopoly ./cmd/server

FROM alpine:3.22
WORKDIR /app
RUN adduser -D -H app && mkdir -p /data && chown app:app /data
COPY --from=build /tutu-monopoly /usr/local/bin/tutu-monopoly
COPY public ./public
USER app
EXPOSE 5510
CMD ["tutu-monopoly"]
