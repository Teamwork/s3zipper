FROM golang:1.15.2-alpine3.12 AS build

WORKDIR /go/src/s3zipper
COPY . .

RUN apk add git
RUN go get -d -v ./...
RUN go install -v ./...

CMD ["s3zipper"]