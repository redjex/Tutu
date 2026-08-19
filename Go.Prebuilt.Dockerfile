FROM alpine:3.22
WORKDIR /app
RUN adduser -D -H app && mkdir -p /data && chown app:app /data
COPY tutu-monopoly /usr/local/bin/tutu-monopoly
COPY public ./public
USER app
EXPOSE 5510
CMD ["tutu-monopoly"]
