FROM golang:1.10.2-stretch
RUN apt-get update --yes && apt-get upgrade --yes
RUN apt-get install --yes pkg-config bash git build-essential
RUN git clone https://github.com/edenhill/librdkafka.git && cd librdkafka && git checkout tags/v0.11.4  && ./configure --prefix /usr && make && make install
RUN mkdir -p /usr/src/app
WORKDIR /usr/src/app
RUN go get "github.com/rcrowley/go-metrics"
RUN go get gopkg.in/redis.v4
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
