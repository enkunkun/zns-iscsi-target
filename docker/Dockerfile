# Stage 1: Build Go binary
FROM golang:1.23-alpine AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/zns-iscsi ./cmd/zns-iscsi

# Stage 2: Build React dashboard
FROM node:20-alpine AS web-builder
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

# Stage 3: Runtime
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=go-builder /out/zns-iscsi /usr/local/bin/
COPY --from=web-builder /web/dist /var/lib/zns-iscsi/web/
EXPOSE 3260 8080
ENTRYPOINT ["/usr/local/bin/zns-iscsi"]
CMD ["--config", "/etc/zns-iscsi/config.yaml"]
