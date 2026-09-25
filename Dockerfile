# syntax=docker/dockerfile:1

FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/server /app/server
COPY migrations /app/migrations
ENV MIGRATIONS_DIR=/app/migrations
ENV HTTP_ADDR=:8080
EXPOSE 8080
USER nobody
ENTRYPOINT ["/app/server"]
