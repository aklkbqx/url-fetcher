FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN if [ ! -f go.mod ]; then go mod init url-fetcher; fi

RUN go get -u github.com/go-sql-driver/mysql

COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o url-fetcher

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/url-fetcher .

CMD ["./url-fetcher"]