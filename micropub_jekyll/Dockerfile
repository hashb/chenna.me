FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 go build -o /micropub-jekyll .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates git openssh-client

COPY --from=builder /micropub-jekyll /micropub-jekyll

RUN mkdir -p /data

EXPOSE 8080

CMD ["/micropub-jekyll"]
