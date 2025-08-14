FROM golang:1.24.5-alpine AS builder
ARG SVC
ARG GOARCH
ARG GOARM
ARG VERSION
ARG COMMIT
ARG TIME

WORKDIR /go/src/github.com/absmach/propeller
COPY . .
RUN apk update \
    && apk add make upx\
    && make $SVC \
    && upx build/$SVC \
    && mv build/$SVC /exe

FROM scratch
COPY --from=builder /exe /
ENTRYPOINT ["/exe"]
