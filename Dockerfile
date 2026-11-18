FROM golang:1.27-alpine AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=$GOPROXY CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags '-s -w' -o /out/pichat ./cmd/pichat

FROM alpine:3
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S -g 10001 pichat && adduser -S -u 10001 -G pichat -h /data pichat \
 && mkdir -p /data && chown pichat:pichat /data
COPY --from=build /out/pichat /usr/local/bin/pichat
USER pichat
ENV PICHAT_LISTEN=0.0.0.0:8080 PICHAT_DATA=/data GOMEMLIMIT=64MiB
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD wget -qO- http://localhost:8080/healthz || exit 1
ENTRYPOINT ["pichat"]
CMD ["serve"]
