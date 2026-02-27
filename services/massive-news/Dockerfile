FROM node:20-alpine

RUN apk add --no-cache tzdata

WORKDIR /app

COPY massive-news.js /app/massive-news.js
COPY run-massive-news.sh /app/run-massive-news.sh
COPY loop.sh /app/loop.sh

RUN chmod +x /app/run-massive-news.sh /app/loop.sh

CMD ["/app/loop.sh"]
