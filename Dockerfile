# ---- build stage ----
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY . .
# go.sum isn't checked in here since this scaffold was authored offline;
# `go mod tidy` resolves and locks versions during the image build.
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api

# ---- runtime stage ----
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/api ./api

EXPOSE 8080
ENTRYPOINT ["./api"]
