FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# No CGO needed: the SQLite driver is pure Go, so the binary is static
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/pasyot-launcher .

FROM alpine:3
WORKDIR /app
# ca-certificates is required to reach Vedrow over https; nothing else is needed
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/pasyot-launcher .
COPY web/ ./web/
EXPOSE 8081
CMD ["./pasyot-launcher"]
