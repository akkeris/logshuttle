FROM golang:1.12.6-alpine3.10
RUN apk update && apk upgrade
RUN apk add --no-cache pkgconfig bash git build-base librdkafka-dev dep
WORKDIR /go/src/logshuttle
COPY . .
ENV GO111MODULE on
RUN go build .
CMD ["./logshuttle"]
EXPOSE 5000
