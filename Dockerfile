ARG GO_VERSION=1.27.3
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/trips-api ./cmd/api

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/trips-api /usr/local/bin/trips-api
EXPOSE 8080
ENTRYPOINT ["trips-api"]