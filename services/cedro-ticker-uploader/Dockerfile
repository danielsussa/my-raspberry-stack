FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN go build -o /out/server ./main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/server /app/server
VOLUME ["/data/cedro-ticker-uploader"]
CMD ["/app/server"]
