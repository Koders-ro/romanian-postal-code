# The Go image is pulled from Google's Docker Hub mirror, so builds don't depend
# on Docker Hub (auth outages, pull rate limits). It is the same official image.
# Override with: --build-arg GO_IMAGE=golang:1.25-alpine
ARG GO_IMAGE=mirror.gcr.io/library/golang:1.25-alpine

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
COPY data ./data
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION}" -o /out/server ./cmd/server

# The binary is static and the dataset is embedded: nothing else is needed.
FROM scratch
COPY --from=build /out/server /server
USER 65534:65534
ENV PORT=3000
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/server", "-healthcheck"]
STOPSIGNAL SIGTERM
ENTRYPOINT ["/server"]
