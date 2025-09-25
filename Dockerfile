FROM golang:1.23.12-alpine

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN apk add --no-cache git

RUN go build -v -o s3zipper

CMD ["./s3zipper"]
