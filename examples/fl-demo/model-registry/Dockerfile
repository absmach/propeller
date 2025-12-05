FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum* ./
COPY main.go ./
RUN go mod tidy && go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -o model-registry .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/model-registry .

CMD ["./model-registry"]
