FROM node:20-alpine

RUN apk add --no-cache git tzdata \
  && npm install -g @openai/codex

WORKDIR /workspace

COPY run-notes.sh /app/run-notes.sh
COPY loop.sh /app/loop.sh

RUN chmod +x /app/run-notes.sh /app/loop.sh

CMD ["/app/loop.sh"]
