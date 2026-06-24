# syntax=docker/dockerfile:1
# Shared build for every /cmd binary; SERVICE selects which one to build.
FROM golang:1.26 AS build
ARG SERVICE
WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -o /out/service ./cmd/${SERVICE}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/service /service
USER nonroot:nonroot
ENTRYPOINT ["/service"]
