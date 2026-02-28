FROM node:20-alpine

RUN apk add --no-cache git tzdata \
  && npm install -g @openai/codex

WORKDIR /workspace

COPY run-intraday.sh /app/run-intraday.sh
COPY loop.sh /app/loop.sh

RUN chmod +x /app/run-intraday.sh /app/loop.sh

CMD ["/app/loop.sh"]
