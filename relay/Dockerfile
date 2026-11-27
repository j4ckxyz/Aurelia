# The relay, in an image of its own binary alone.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /aurelia-relay .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /aurelia-relay /aurelia-relay
EXPOSE 8787
ENTRYPOINT ["/aurelia-relay"]
CMD ["-addr", ":8787"]
