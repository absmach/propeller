FROM golang:1.25.5-alpine3.22 AS builder
ARG SVC
ARG GOARCH
ARG GOARM
ARG VERSION
ARG COMMIT
ARG TIME

WORKDIR /go/src/github.com/absmach/propeller
COPY . .
RUN apk update \
    && apk add make upx curl build-base \
    && curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y \
    && export PATH="$HOME/.cargo/bin:$PATH" \
    && make $SVC \
    && if [ -f "build/$SVC" ]; then upx build/$SVC && mv build/$SVC /exe; \
       elif [ -f "proplet/target/release/$SVC" ]; then upx proplet/target/release/$SVC && mv proplet/target/release/$SVC /exe; \
       elif [ -f "target/release/$SVC" ]; then upx target/release/$SVC && mv target/release/$SVC /exe; \
       else echo "Binary not found" && find . -name "$SVC" -type f 2>/dev/null && exit 1; fi

FROM scratch
LABEL org.opencontainers.image.source=https://github.com/absmach/propeller
LABEL org.opencontainers.image.licenses=Apache-2.0

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /exe /
ENTRYPOINT ["/exe"]
