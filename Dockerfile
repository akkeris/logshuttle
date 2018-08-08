FROM golang:1.10.2-alpine3.7
RUN apk update && apk upgrade
RUN apk add --no-cache pkgconfig bash git build-base
RUN git clone https://github.com/edenhill/librdkafka.git && cd librdkafka && git checkout tags/v0.11.4  && ./configure --prefix /usr && make && make install
RUN mkdir -p /usr/src/app
WORKDIR /usr/src/app
RUN go get gopkg.in/redis.v4
RUN go get "github.com/lib/pq"
RUN go get "github.com/rcrowley/go-metrics"
RUN go get github.com/go-martini/martini
RUN go get github.com/martini-contrib/auth
RUN go get github.com/martini-contrib/binding
RUN go get github.com/martini-contrib/render
RUN go get github.com/nu7hatch/gouuid
RUN go get github.com/confluentinc/confluent-kafka-go/kafka
RUN go get github.com/stackimpact/stackimpact-go
RUN go get gopkg.in/mcuadros/go-syslog.v2
RUN go get github.com/smartystreets/goconvey/convey
COPY . /usr/src/app
RUN go build -o /usr/src/app/server .
CMD ["/usr/src/app/server"]
EXPOSE 5000
