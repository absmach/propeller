FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum* ./
RUN go get github.com/gorilla/mux@v1.8.1 && go mod download

COPY main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -o local-data-store main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/local-data-store .

EXPOSE 8083

CMD ["./local-data-store"]
