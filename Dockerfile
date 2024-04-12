FROM golang:1.22.0-alpine3.19
RUN apk update
RUN apk add --no-cache ca-certificates
ENV CGO_ENABLED=0
COPY go.mod go.sum .
COPY . .
RUN go build -o /bin/dispatch .
ENTRYPOINT ["/bin/dispatch"]
