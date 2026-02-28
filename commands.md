docker compose up -d --build market-visual-runner-bff
docker compose up -d --build massive-news


sudo chown -R $(id -u):$(id -g) /home/pi/stack/.data
services/intraday-ai-predict/copy.sh "2026-02-25 16:25:00" "2026-02-27 16:24:00"