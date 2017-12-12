package main

import (
	"time"
	"./drains"
	cluster "github.com/bsm/sarama-cluster"
)

type LabelsSpec struct {
	Name string `json:"name"`
	PodTemplateHash string `json:"pod-template-hash"`
}

type KubernetesSpec struct {
	NamespaceName string `json:"namespace_name"`
	PodId string `json:"pod_id"`
	PodName string `json:"pod_name"`
	ContainerName string `json:"container_name"`
	Labels LabelsSpec `json:"labels"`
	Host string `json:"host"`
}

type DockerSpec struct {
	ContainerId string `json:"container_id"`
}

type LogSpec struct {
	Log string `json:"log"`
	Stream string `json:"stream"`
	Time time.Time `json:"time"`
	Space string `json:"space"`
	Docker DockerSpec `json:"docker"`
	Kubernetes KubernetesSpec `json:"kubernetes"`
	Topic string `json:"topic"`
	Tag string `json:"tag"`
}

type Route struct {
	Id string `json:"id"`
	Space string `json:"space"`
	App string `json:"app"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	DestinationUrl string `json:"url"`
	Destination *drains.Drain
}

type LogDrainCreateRequest struct {
	Url string `json:"url"`
}

type AddonResponse struct {
	Id string `json:"id"`
	Name string `json:"name"`
}

type LogDrainResponse struct {
	Addon AddonResponse `json:"addon"`
	CreatedAt time.Time `json:"created_at"`
	Id string `json:"id"`
	Token string `json:"token"`
	UpdatedAt time.Time `json:"updated_at"`
	Url string `json:"url"`
}

type MessageMetadata struct {
	ReceivedAt time.Time `json:"received_at"`
}

type KafkaMessage struct {
	Partition int32           `json:"partition"`
	Offset    int64           `json:"offset"`
	Value     string          `json:"value"`
	Metadata  MessageMetadata `json:"metadata"`
}

type LogSession struct {
	App string `json:"app"`
	Space string `json:"space"`
	Lines int `json:lines`
	Tail bool `json:tail`
}

type Process struct {
	App string `json:"app"`
	Type string `json:"type"`
}

type KafkaConsumer struct {
	Consumer *cluster.Consumer
	Config *cluster.Config
	Client *cluster.Client
}
