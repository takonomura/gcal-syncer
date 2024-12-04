FROM docker.io/library/golang:1.23.4-alpine@sha256:9bfa54e3a85920df66ecf1c4979dfdf88baa80ca9752adda442713863087ef18 AS build

RUN apk add --no-cache ca-certificates && update-ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 go build -v -ldflags '-s -w' -trimpath .

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /app/gcal-syncer /gcal-syncer

CMD ["/gcal-syncer"]
