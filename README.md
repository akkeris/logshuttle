# Akkeris Log Shuttle and API #

## Setting Up ##

You may need to run `git config --global http.https://gopkg.in.followRedirects true` as golang has an issue following redirects on gopkg.in.

**Important** Never shuttle the logs for the actual log shuttle, this may result in an infinite loop if theres an error in routing the logs!

- **RUN_SESSION** - Indicates if we wish to use the log session end point rather than the log shuttle end point (See Log Session below for rational), if you're looking to shuttle logs do not enable this. If you do want a log session end point (and a log session end point only) set this to 1.  Note enabling this will disable the log shuttle end point.  These two end points are mutually exclusive due to the burden it puts on the app and the completely separate types of workloads shuttling vs. sessions need to do.

### Storage ###

- **REDIS_URL** - The redis url to maintain what to shuttle and where to.  It also stores temporary log sessions produced.

### Security ###

- **AUTH_KEY** - The authentication key to use, any requests without this will result in a 401 Unauthorized, the key should be passed in as the value to the "Authorzation" header (no encoding required)

### Log Information ###

- **KAFKA_HOSTS** - The comma delimited list of hosts (and optionally port concatenated with a : proceeding the host, e.g., host:port) of the kafka instances (not the zookeepers), to connect to.  Each topic must be a "space", the values in the topic must be the kubernetes JSON log structure. (see below).
- **PORT** - Defaults to 5000, the port to listen to for API calls.
- **SYSLOG** - The default syslog end point for the log shuttle's logs. 

### Incoming Log Structure ###

The incoming value for all messages must be (standard for kubernetes) the following:

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

## Usage ##

This API should sit behind the alamo-app-controller process, it should not be hit directly expect for testing.

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
