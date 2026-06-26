FROM golang:1.26.4 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/almanaut .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/almanaut /almanaut
VOLUME ["/data"]
ENV ALMANAUT_DATA_DIR=/data ALMANAUT_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/almanaut"]
