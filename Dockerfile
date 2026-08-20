FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/streamer ./cmd/streamer

FROM alpine:3.22
RUN apk add --no-cache ca-certificates ffmpeg tzdata
WORKDIR /app
COPY --from=build /out/streamer /usr/local/bin/streamer
RUN addgroup -S app && adduser -S -G app app && mkdir -p /app/output /app/work && chown -R app:app /app
USER app
EXPOSE 8088
ENTRYPOINT ["streamer"]
