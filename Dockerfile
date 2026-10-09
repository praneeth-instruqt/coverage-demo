# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/todo-server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/todo-server /todo-server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/todo-server"]
