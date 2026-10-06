FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.6.1 AS xx

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
COPY --from=xx / /
RUN apk add --no-cache clang lld make
WORKDIR /src
COPY go.mod go.sum Makefile VERSION ./
RUN make deps && go mod download
ARG TARGETPLATFORM
RUN xx-apk add --no-cache gcc musl-dev
COPY . .
RUN xx-go --wrap && make build EXTRA_LDFLAGS="-linkmode=external -extldflags=-static" && xx-verify --static build/tossling-server

FROM alpine:3
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /src/build/tossling-server /usr/local/bin/tossling-server
COPY --from=build /src/LICENSE /src/NOTICE /usr/share/doc/tossling-server/
COPY --from=build /src/licenses /usr/share/doc/tossling-server/licenses
ENV TOSSLING_DATA=/data TOSSLING_LISTEN=:8090
VOLUME /data
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD wget -qO- http://127.0.0.1:8090/v1/tossy/health >/dev/null || exit 1
ENTRYPOINT ["tossling-server"]
