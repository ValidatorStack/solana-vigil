FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /vigil ./cmd/vigil

FROM scratch
USER 65532:65532
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /vigil /vigil
ENTRYPOINT ["/vigil"]
CMD ["--help"]
