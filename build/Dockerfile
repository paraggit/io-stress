FROM golang:1.26 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o /out/odf-io-stress ./cmd/odf-io-stress

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
ARG SOURCE_URL=https://github.com/paraggit/io-stress
LABEL org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.title="odf-io-stress" \
      org.opencontainers.image.description="ODF IO stress object worker"
COPY --from=build /out/odf-io-stress /usr/local/bin/odf-io-stress
ENTRYPOINT ["/usr/local/bin/odf-io-stress"]
