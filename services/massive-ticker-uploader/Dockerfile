FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY go.sum ./
COPY main.go ./
RUN go mod download
RUN go build -o /out/server ./main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=build /out/server /app/server
VOLUME ["/data/massive-ticker-uploader"]
CMD ["/app/server"]
