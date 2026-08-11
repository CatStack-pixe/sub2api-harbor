FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/heartbeat-bridge .

FROM alpine:3.21
RUN addgroup -S app && adduser -S -G app app
COPY --from=build /out/heartbeat-bridge /usr/local/bin/heartbeat-bridge
USER app
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/heartbeat-bridge"]
