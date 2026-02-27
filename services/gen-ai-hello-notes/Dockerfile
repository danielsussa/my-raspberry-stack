FROM alpine:3.20

RUN apk add --no-cache curl jq tzdata

WORKDIR /app

COPY run-notes.sh /app/run-notes.sh
COPY crontab /etc/crontabs/root

RUN chmod +x /app/run-notes.sh

CMD ["crond", "-f", "-l", "8", "-L", "/dev/stdout"]
