FROM golang:1.24-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/stds-api ./cmd/api

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
	&& addgroup -S stds \
	&& adduser -S -G stds stds

WORKDIR /app

COPY --from=builder /out/stds-api /app/stds-api

ENV APP_HOST=0.0.0.0 \
	APP_PORT=8080 \
	GIN_MODE=release

EXPOSE 8080

USER stds

ENTRYPOINT ["/app/stds-api"]
