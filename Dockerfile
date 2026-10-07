# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/mediaphile-server ./cmd/mediaphile-server

FROM alpine:3.22
RUN apk add --no-cache ca-certificates ffmpeg && addgroup -S mediaphile && adduser -S -G mediaphile mediaphile
WORKDIR /app
COPY --from=build /out/mediaphile-server /usr/local/bin/mediaphile-server
RUN mkdir -p /config /transcode /ui /media && chown -R mediaphile:mediaphile /config /transcode /ui
USER mediaphile
ENV MEDIAPHILE_LISTEN_ADDR=0.0.0.0 MEDIAPHILE_DATA_DIR=/config MEDIAPHILE_TRANSCODE_DIR=/transcode MEDIAPHILE_UI_DIR=/ui
EXPOSE 8097/tcp 8098/udp
ENTRYPOINT ["/usr/local/bin/mediaphile-server"]
