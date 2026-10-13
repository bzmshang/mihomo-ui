# fetch the panel (zashboard) that the binary embeds
FROM alpine:3.20 AS panel
RUN apk add --no-cache bash curl unzip coreutils
WORKDIR /src
COPY scripts/fetch-zashboard.sh scripts/
RUN bash scripts/fetch-zashboard.sh

# build control plane
FROM golang:1.22-alpine AS api
WORKDIR /src
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ .
# The panel is embedded into the binary, so it must be in place first.
COPY --from=panel /src/backend/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -o /out/mihomo-ui ./cmd/server

# final: official mihomo + UI (single process: mihomo-ui starts mihomo)
FROM metacubex/mihomo:latest

RUN apk add --no-cache ca-certificates tzdata || true

COPY --from=api /out/mihomo-ui /usr/local/bin/mihomo-ui

# Single home: mount host dir -> /data/mihomo-ui
ENV TZ=Asia/Shanghai \
    UI_LISTEN=0.0.0.0:7080 \
    MIHOMO_BIN=/mihomo \
    DATA_HOME=/data/mihomo-ui

VOLUME ["/data/mihomo-ui"]
EXPOSE 7080

ENTRYPOINT ["/usr/local/bin/mihomo-ui"]
