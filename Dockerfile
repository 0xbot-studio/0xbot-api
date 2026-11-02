FROM golang:1.26.8-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/server
FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -g 10001 app && adduser -D -u 10001 -G app app
COPY --from=build /out/api /usr/local/bin/0xbot-api
USER 10001:10001
EXPOSE 8095
CMD ["/usr/local/bin/0xbot-api"]

