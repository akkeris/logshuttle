FROM golang:1.9-alpine
RUN apk update && apk upgrade && apk add --no-cache bash git openssh librdkafka pkg-config
RUN mkdir -p /usr/src/app
WORKDIR /usr/src/app
COPY . /usr/src/app
RUN go get "github.com/rcrowley/go-metrics"
RUN go get gopkg.in/redis.v4
RUN go get github.com/go-martini/martini
RUN go get github.com/martini-contrib/auth
RUN go get github.com/martini-contrib/binding
RUN go get github.com/martini-contrib/render
RUN go get github.com/nu7hatch/gouuid
RUN go get github.com/confluentinc/confluent-kafka-go/kafka
RUN go get github.com/stackimpact/stackimpact-go
RUN go build -o /usr/src/app/server .
CMD ["/usr/src/app/server"]
EXPOSE 5000
