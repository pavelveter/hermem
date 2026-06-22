FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -o /hermem .

FROM alpine:3.21
RUN adduser -D -h /home/hermem hermem
COPY --from=builder /hermem /usr/local/bin/hermem
USER hermem
WORKDIR /home/hermem
EXPOSE 8420
ENTRYPOINT ["hermem"]
CMD ["serve"]
