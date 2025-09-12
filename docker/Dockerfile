FROM golang:1.25-alpine AS builder
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

FROM ubuntu:25.10
LABEL org.opencontainers.image.source=https://github.com/absmach/propeller
LABEL org.opencontainers.image.licenses=Apache-2.0
RUN apt-get update && apt-get install -y wget xz-utils
RUN wget https://github.com/bytecodealliance/wasmtime/releases/download/v36.0.2/wasmtime-v36.0.2-x86_64-linux.tar.xz
RUN tar -xvf wasmtime-v36.0.2-x86_64-linux.tar.xz
RUN cp wasmtime-v36.0.2-x86_64-linux/wasmtime /usr/local/bin
COPY --from=builder /exe /
ENTRYPOINT ["/exe"]
