FROM golang:1.26.4 AS build
WORKDIR /src
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=${VERSION}" -o /out/almanaut .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/almanaut /almanaut
VOLUME ["/data"]
ENV ALMANAUT_DATA_DIR=/data ALMANAUT_ADDR=:8080
EXPOSE 8080
# The distroless image has no shell, so the binary probes its own /healthz.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
	CMD ["/almanaut", "healthcheck"]
ENTRYPOINT ["/almanaut"]
