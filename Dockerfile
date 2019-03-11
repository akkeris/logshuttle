FROM golang:1.11.5-alpine3.9
RUN apk update && apk upgrade
RUN apk add --no-cache pkgconfig bash git build-base librdkafka-dev dep
WORKDIR /go/src/logshuttle
COPY . .
RUN dep ensure --add github.com/go-martini/martini@22fa46961aabd2665cf3f1343b146d20028f5071
RUN go build .
CMD ["/logshuttle"]
EXPOSE 5000
