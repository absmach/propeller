FROM golang:1.26-bookworm AS builder
ARG SVC
# TARGETARCH / TARGETVARIANT are set automatically by docker buildx.
# GOARCH / GOARM can still be passed explicitly via --build-arg to keep
# backwards-compatibility with the `make dockers` Makefile targets.
ARG TARGETARCH
ARG TARGETVARIANT
ARG GOARCH=${TARGETARCH}
ARG GOARM
ARG VERSION
ARG COMMIT
ARG TIME

WORKDIR /go/src/github.com/absmach/propeller
COPY . .
RUN apt-get update \
    && apt-get install -y --no-install-recommends make \
    && _goarm="${GOARM:-${TARGETVARIANT#v}}" \
    && make $SVC GOARCH=${GOARCH} ${_goarm:+GOARM=${_goarm}} \
    && mv build/$SVC /exe

FROM gcr.io/distroless/cc-debian12
LABEL org.opencontainers.image.source=https://github.com/absmach/propeller
LABEL org.opencontainers.image.licenses=Apache-2.0

COPY --from=builder /exe /
ENTRYPOINT ["/exe"]
