FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 go build -o /guestbook-backend .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /guestbook-backend /guestbook-backend

RUN mkdir -p /data

EXPOSE 8080

CMD ["/guestbook-backend"]
