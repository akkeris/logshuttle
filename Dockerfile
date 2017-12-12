FROM golang:1.9-alpine
RUN apk update && apk upgrade && apk add --no-cache bash git openssh
RUN mkdir -p /usr/src/app
WORKDIR /usr/src/app
COPY . /usr/src/app
RUN go get "github.com/rcrowley/go-metrics"
RUN go get "github.com/Shopify/sarama"
RUN go get "github.com/bsm/sarama-cluster"
RUN go get "github.com/papertrail/remote_syslog2/syslog"
RUN go get gopkg.in/redis.v4
RUN go get github.com/go-martini/martini
RUN go get github.com/martini-contrib/auth
RUN go get github.com/martini-contrib/binding
RUN go get github.com/martini-contrib/render
RUN go get github.com/nu7hatch/gouuid
RUN go get github.com/trevorlinton/remote_syslog2
RUN go build -o /usr/src/app/server .
CMD ["/usr/src/app/server"]
EXPOSE 5000
