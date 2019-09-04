FROM golang:1.12.9-alpine3.10
RUN apk update && apk upgrade
RUN apk add --no-cache pkgconfig bash git build-base dep curl
WORKDIR /tmp
RUN curl -O http://packages.confluent.io/archive/5.3/confluent-community-5.3.0-2.12.tar.gz
RUN tar zxvf confluent-community-5.3.0-2.12.tar.gz
RUN tar zxvf confluent-5.3.0/src/librdkafka-1.1.0-confluent5.3.0.tar.gz
WORKDIR /tmp/librdkafka-1.1.0-confluent5.3.0
RUN ./configure --install-deps
RUN make
RUN make install
WORKDIR /go/src/logshuttle
COPY . .
ENV GO111MODULE on
RUN go build .
CMD ["./logshuttle"]
EXPOSE 5000
