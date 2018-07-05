# Akkeris Log Shuttle and API #

[![CircleCI](https://circleci.com/gh/akkeris/logshuttle.svg?style=svg)](https://circleci.com/gh/akkeris/logshuttle)

The log shuttle allows you to dynamically route logging from your build, app and web logs into a variety of different places by adding `drains` via its JSON API's.  In addition specific low-volume logs (such as systme events, or platform events) can be added to drains via the API as well.

Logs from fluentd within kubernetes, jenkins (via the kafkalogs plugin) from builds and http logs (from any generic F5, nginx or apache server) can be pushed into kafka where logshuttle picks up, parses, and delivers logs to specified end points that can be dynamically added or removed via the log shuttle API.  The end points that logs can be routed to must be a `syslog+tls://`, `syslog+tcp://`, `syslog+udp://` or `https` schema type.  Note that specific meta data must be added to fluentd, jenkins as well as a convention within the http logs in order for logshuttle to properly identify an "app" that should be routed.

## Setting Up ##

You may need to run `git config --global http.https://gopkg.in.followRedirects true` as golang has an issue following redirects on gopkg.in. Official docker images can be found on [docker.io](https://hub.docker.com/r/akkeris/logshuttle/tags/)

### General Settings ###

- **TEST_MODE** - If set to any value this sets the logshuttle into testing mode that will give it a consumer group name different than the normal log shuttle to not interfer with existing logshuttles; in addition it will override the host in all outgoing destinations to "logshuttle-test" so that it will not be collected (or at a bare minimum, it can be extracted) from existing log end points.
- **PORT** - Defaults to 5000, the port to listen to for API calls.
- **RUN_SESSION** - Indicates if we wish to use the log session end point rather than the log shuttle end point (See Log Session below for rational), if you're looking to shuttle logs do not enable this. If you do want a log session end point (and a log session end point only) set this to 1.  Note enabling this will disable the log shuttle end point.  These two end points are mutually exclusive due to the burden it puts on the app and the completely separate types of workloads shuttling vs. sessions need to do.
- **SESSION_URL** - This should be set to the log sessions public dns host e.g., https://logsession.example.com

### Storage Settings ###

- **REDIS_URL** - The redis url to maintain what to shuttle and where to.  It also stores temporary log sessions produced. Note either this or POSTGRES_URL must be set.
- **POSTGRES_URL** - The postgres url to maintain what to shuttle and where to.  It also stores temporary log sessions produced. Note either this or REDIS_URL must be set.

### Security Settings ###

- **AUTH_KEY** - The authentication key to use, any requests without this will result in a 401 Unauthorized, the key should be passed in as the value to the "Authorzation" header (no encoding required)

### Stream Settings ###

- **KAFKA_HOSTS** - The comma delimited list of hosts (and optionally port concatenated with a : proceeding the host, e.g., host:port) of the kafka instances (not the zookeepers), to connect to.  Each topic must be a "space", the values in the topic must be the kubernetes JSON log structure. (see below). 

**Important Kafka Notes**: for the consistency in the ordering of logs (and scalability), the fluentd configuration from kubernetes kicking logs into kafka MUST partition its key based on the container name, in addition there MUST be as many logshuttle instances as there are partitions in kafka and must scale in conjunction for optimal performance.


### Setting up Fluentd App Logs ###

Fluentd is used to push logs from kubernetes into kafka and subsequently into the logshuttle for distribution to one or more syslog end points.  Fluentd should be deployed as a daemon set on your cluster on each node with the recommended configuration (note this assumes fluentd 14), note to use this fluentd configuration the environment variables `ZOO_IPS` a comma delimited list of the kafka broker ips addresses must be set in config map for the fluentd daemonset.  The logshuttle defines an app as a `${deployment name}-${namespace name}` in kubernetes.  So for instance, if you had a namespace "foo" and a deployment within it as "bar" to add a syslog end point via the JSON api the app name would be "bar-foo".

```
<match fluent.**>
  @type null
</match>

<source>
  @type tail
  refresh_interval 1
  path /var/log/containers/*.log
  pos_file /var/log/es-containers.log.pos
  time_key @timestamp
  time_format %Y-%m-%dT%H:%M:%S.%L%Z
  tag kubernetes.*
  format json
  read_from_head true
</source>

<source>
  @type monitor_agent
  bind 0.0.0.0
  port 24220
</source>

<filter kubernetes.**>
  @type kubernetes_metadata
</filter>


<filter **>
  @type record_transformer
  enable_ruby
  <record>
    topic ${kubernetes["namespace_name"]}
    partition_key ${kubernetes["container_name"]}
  </record>
</filter>

<filter **>
  @type grep
  <exclude>
    key topic
    pattern kube-system
  </exclude>
</filter>

<match **>
  @type kafka_buffered
  zookeeper ZOO_IPS # Set brokers via Zookeeper
  zookeeper_path /kafka/brokers/ids
  default_topic default
  output_data_type json
  output_include_tag  false
  output_include_time false
  max_send_retries  3
  required_acks 0
  ack_timeout  1500
  flush_interval 2s
  buffer_queue_limit 128
  num_threads 2
</match>
```


The incoming structure for logs pushed from fluentd to kafka should look like (and be placed in a topic matching the namespace):

```json
  {
    "log":"Some logging message",
    "stream":"stderr",
    "time":"2016-08-24T17:03:20.080151664Z",
    "docker": {
      "container_id":"790e3ac96bc32b0bedaa95c53034cc7f135b58bf2337257e91040a0f02f09110"
    },
    "kubernetes": {
      "namespace_name":"space",
      "pod_id":"539194ff-6634-11e6-b7ad-06468cfee103",
      "pod_name":"app-instance-friendly-id",
      "container_name":"app",
      "labels":{"name":"some-labels"},
      "host":"ip-1-1-1-1.somehost.somedomain.com"
    },
    "topic":"space",
    "tag":"some-tags.log"
  }
```

The pod_name should be an "app-instance-friendly-id", e.g., a friendly id to designate the individual running app server, meaning if you have two web servers running, this would be web.1, or web.2. The container_name should equal the app name, the topic should equal the space. The namespace_name should also be the space. The log field should contain the information to be forwarded with the stream field containing what stream (stdout, stderr) the information came from.  Finally, the "time" field should be a time that contains a nano-second ISO time stamp (although, lower resolution time stamps are acceptable).


### Setting up Jenkins Build Logs ###

**With The Build Shuttle**

Note if you are using the akkeris buildshuttle system, no extra steps are required to stream build logs.

**Without the Build Shuttle but With Jenkins**

You'll need to install the kafkalogs plugin, and the builds must be a DSL/pipeline build system.  Use the following wrapper:

```
withKafkaLog(kafkaServers: 'host1.example.com:9092,host2.example.com:9092', kafkaTopic: 'alamobuildlogs', metadata:'appname') {
  echo 'Your build steps here..'
  echo 'Anoter build log line..'
}
```

The specified app name in the metadata corresponds to the app name below in the JSON API for the logshuttle.

**Without Jenkins or the Build Shuttle**

If you are not using the build shuttle, build logs must be pushed into the `alamobuildlogs` topic of kafka with the following format:

`{"build":40,"job":"test","message":"Hello World","metadata":"deploymentname-namespacename"}`

Where build is the numeric build number, job is the job name that is running, message is the build log message line and metadata should contain the deployment name and the namespace name. The combination of the deploymentname-namespacename becomes the app name within the JSON API below. For example, if you had a deployment named "abc" within the space "default" with pod abc.1342234-34djh2 then the app name would be "abc-default".


### Setting up HTTP web logs ###

http logs will be automatically processed if they are streamed to the `alamoweblogs` topic (there are plenty of existing open soruce tools to stream http log files to a kafka topic).  The one caveat is the log must have the host=[appname].somedomain.com. The app name should correspond to the kubernetes deploymentname-namespacename. 

## API Usage ##

This API should sit behind the controller-api process, it should not be hit directly except for testing.

## API General Information ##

# Log Drains #

## Add a Log Drains ##

### POST /apps/{appname}/log-drains

Create a new log drain, log drains allow you to push logs to another syslogd on another host (such as papertrail or an internal syslogd instance listening on a port).

The only required field in the post is the URL to push data to, the data should have one of the following schemas:

* syslog+tls:// - Push to a SSL (TLS technically) end point with syslogd format.
* syslog:// - Push to a unencrypted TCP end point with syslogd format (note this is not secure, and is not recommended).
* syslog+udp:// - Push to an unencrypted UDP end point with syslogd format (note this may result in out of order logs, is not secure and is not recommended).


|   Name   |       Type      | Description                                                                                                                                                                                                | Example                                                                                                                            |
|:--------:|:---------------:|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|------------------------------------------|
|  url   | required string | The url that contains where to route information to (see above for acceptable schemas). | syslog+tls://logs.papertrailapp.com:44112 |


**CURL Example**

```bash
curl \
  -H 'Authorization: ...' \
  -X POST \
  https://hostname/apps/{appname}/log-drains \
  -d '{"url":"syslog+tls://logs.somelogging.com:34243"}'
```

**201 "Created" Response**

```json
{  
  "created_at":"2016-07-18T14:55:38.190Z",
  "id":"4f739e5e-4cf7-11e6-beb8-9e71128cae77",
  "addon":{  
     "id":"c164e2c4-958b-a141-d5f4-133a33f0688f",
     "name":"logdrain-sci"
  },
  "updated_at":"2016-07-18T14:55:38.190Z",
  "token":"",
  "url":"syslog+tls://logs.somelogging.com:34243"
}
```

## Delete a Log Drain ##

Disconnects a log drain from forwarding.

### DELETE /apps/{appname}/log-drains/{log_drain_id}

**CURL Example**

```bash
curl \
  -H 'Authorization: ...' \
  -X DELETE \
  https://hostname/apps/{appname}/log-drains/4f739e5e-4cf7-11e6-beb8-9e71128cae77
```

**200 "OK" Response**

```json
{  
  "created_at":"2016-07-18T14:55:38.190Z",
  "id":"4f739e5e-4cf7-11e6-beb8-9e71128cae77",
  "addon":{  
     "id":"c164e2c4-958b-a141-d5f4-133a33f0688f",
     "name":"logdrain-sci"
  },
  "updated_at":"2016-07-18T14:55:38.190Z",
  "token":"",
  "url":"syslog+tls://logs.somelogging.com:34243"
}
```

## Log Drain Info ##

Gets information on a current log drain.

### GET /apps/{appname}/log-drians/{log drain uuid}

**CURL Example**

```bash
curl \
  -H 'Authorization: ...' \
  https://hostname/apps/{appname}/log-drains/4f739e5e-4cf7-11e6-beb8-9e71128cae77
```

**200 "OK" Response**

```json
{  
  "created_at":"2016-07-18T14:55:38.190Z",
  "id":"4f739e5e-4cf7-11e6-beb8-9e71128cae77",
  "addon":{  
     "id":"c164e2c4-958b-a141-d5f4-133a33f0688f",
     "name":"logdrain-sci"
  },
  "updated_at":"2016-07-18T14:55:38.190Z",
  "token":"",
  "url":"syslog+tls://logs.somelogging.com:34243"
}
```

## Log Drain List ##

Lists all the log drains for an app.

### GET /apps/{appname}/log-drains

**CURL Example**

```bash
curl \
  -H 'Authorization: ...' \
  https://hostname/apps/{appname}/log-drains
```

**200 "OK" Response**

```json
[
  {  
    "created_at":"2016-07-18T14:55:38.190Z",
    "id":"4f739e5e-4cf7-11e6-beb8-9e71128cae77",
    "addon":{  
       "id":"c164e2c4-958b-a141-d5f4-133a33f0688f",
       "name":"logdrain-sci"
    },
    "updated_at":"2016-07-18T14:55:38.190Z",
    "token":"",
    "url":"syslog+tls://logs.somelogging.com:34243"
  }
]
```



## Log Events (private) ##

Add a custom log event for an app.  **This is a private event and should only be called by internal alamo systems** This adds new events that do not exist in the internal infrastructure logging to the log queue, these should only be events that are useful for developers reviewing logs and not necessary for infrastructure use (e.g., information about the app behavior)

### POST /log-events

**BODY**

```json
 {
  "log":"Some logging message",
  "stream":"stderr|stdout",
  "time":"ISO Time Stamp (prefer to nanosec)",
  "docker": {
    "container_id":""
  },
  "kubernetes": {
    "namespace_name":"space",
    "pod_id":"",
    "pod_name":"alamo",
    "container_name":"app",
    "labels":{"name":""},
    "host":""
  },
  "topic":"space",
  "tag":""
}
```

# Log Sessions #

In order to start the log sessions use the same environment variables required as above, but set USE_SESSION=1, this will only enable these end points.  The intent for having seperate end points if an environment variable is set is so log sessions can be spun up on seperate servers disconnected entirely from the log shuttles which have a higher priority, log sessions may cause log lived pulling http responses that could cause IO to fill up, this way we can preserve the log shuttle and put the sessions on their own. 

With that said we should be cognizant that both shuttle and sessions share a large amount of code, therefore we bundle the code base together, but allow different apps to be started with an environment variable. These can be decoupled at some point if the code base becomes unweidly.


## Create Log Sessions (private) ##

Create a log session that tail the logs coming from the server.  Note that although the body request for the post contains a lines and tail the only functionality this end point delivers is tail, its up to middleware or end user client to obey the max lines / tail=false use case. 

### POST /log-sessions

```json
{
  "app":"appname",
  "space":"spacename",
  "lines":250,
  "tail":true
}
```

**201 "Created" Response**

```json
{
  "id":"2349-938234-23444-2399-2933"
}
```


Note that the returned id should be returned with the full URL for the log session (e.g., `$HOST/log-sessions/2349-938234-23444-2399-2933`).  The log session end point is not authenticated to allow for other applications hooking into logging.  The end point created with the session id only lives for 5 minutes.

## Consume Log Sessions (private-ish) ##

### GET /log-sessions/{id} ###

This end point is consumed by the client to get a raw tailed log of an app off of the kafka stream. This end point is dynamically created by the Create Log Sessions and only lives for 5 minutes before disappearing, therefore it does not require authentication. It's intended to allow passing the url into other apps that may need to temporarily trail logs. 
