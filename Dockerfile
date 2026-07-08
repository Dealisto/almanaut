FROM golang:1.26.5 AS build
WORKDIR /src
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=${VERSION}" -o /out/almanaut .
# Pre-create the data dir so the final stage can COPY it in with nonroot
# ownership: distroless has no shell to mkdir/chown, and a fresh named volume
# inherits the image directory's ownership.
RUN mkdir -p /out/data

# The :nonroot variant runs as uid/gid 65532 instead of root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/almanaut /almanaut
COPY --from=build --chown=65532:65532 /out/data /data
VOLUME ["/data"]
ENV ALMANAUT_DATA_DIR=/data ALMANAUT_ADDR=:8080
EXPOSE 8080
USER 65532:65532
# The distroless image has no shell, so the binary probes its own /healthz.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
	CMD ["/almanaut", "healthcheck"]
ENTRYPOINT ["/almanaut"]
