package main

import (
	"./drains"
	"time"
)

type LabelsSpec struct {
	Name            string `json:"name"`
	PodTemplateHash string `json:"pod-template-hash"`
}

type KubernetesSpec struct {
	NamespaceName string     `json:"namespace_name"`
	PodId         string     `json:"pod_id"`
	PodName       string     `json:"pod_name"`
	ContainerName string     `json:"container_name"`
	Labels        LabelsSpec `json:"labels"`
	Host          string     `json:"host"`
}

type DockerSpec struct {
	ContainerId string `json:"container_id"`
}

type LogSpec struct {
	Log        string         `json:"log"`
	Stream     string         `json:"stream"`
	Time       time.Time      `json:"time"`
	Space      string         `json:"space"`
	Docker     DockerSpec     `json:"docker"`
	Kubernetes KubernetesSpec `json:"kubernetes"`
	Topic      string         `json:"topic"`
	Tag        string         `json:"tag"`
}

type Route struct {
	Id             string    `json:"id"`
	Space          string    `json:"space"`
	App            string    `json:"app"`
	Created        time.Time `json:"created"`
	Updated        time.Time `json:"updated"`
	DestinationUrl string    `json:"url"`
	Destination    *drains.Drain
}

type LogSession struct {
	App   string `json:"app"`
	Space string `json:"space"`
	Lines int    `json:lines`
	Tail  bool   `json:tail`
}

type Process struct {
	App  string `json:"app"`
	Type string `json:"type"`
}
