const http = require('http')
const https = require('https')
const url = require('url')
const system_name = process.env.ALAMO_APPLICATION
const system_uri = process.env.URL
console.assert(system_uri && system_uri !== "", 'No URL was provided.')
const papertrail_token = process.env.PAPERTRAIL_TOKEN
const influxdb = process.env.INFLUXDB
const port = process.env.PORT || 9000

const timeout_on_search = parseInt(process.env.TIMEOUT_ON_SEARCH || '3600', 10)
const time_to_failure = parseInt(process.env.TIME_TO_FAILURE || '60', 10)

let search_for_app_ids = []
let search_for_http_ids = []

console.assert(system_name, 'The system name was not found.')
console.assert(papertrail_token, 'The papertrial token was not found.')
console.assert(influxdb, 'Influxdb was not found.')

console.log("System:", system_name)

function wait(time) {
  return new Promise((resolve, reject) => {
    setTimeout(() => { resolve() }, time)
  })
}

function fetch(uri, method, headers, data) {
  return new Promise((resolve, reject) => {
    let connector = (uri.startsWith('https') ? https : http);
    let options = Object.assign(url.parse(uri), {headers, method});
    let req = connector.request(options, (res) => {
      let data = Buffer.alloc(0)
      res.on('data', (d) => { data = Buffer.concat([data, d]) });
      res.on('end', () => {
        try {
          resolve(JSON.parse(data.toString()))
        } catch (e) {
          resolve(data.toString())
        }
      })
    })
    req.on('error', reject);
    if(data) {
      req.write(data)
    }
    req.end()
  })
}

async function record(name, metric, successful, drift, begin_date, end_date) {
  if(process.env.TEST_MODE) {
    console.log("=> write " + metric + ' successful=' + successful + ' host=' + name + " drift=" + drift)
  } else {
    if(drift < 0) {
      drift = 0
    }
    await fetch(influxdb + '/write?db=logmonitor&_http_tag=' + name, 'POST', {}, 'logs,type=' + metric + ',successful=' + successful + ',host=' + name + ' drift=' + drift)
    await wait(100)
  }
}

async function search_for_record(search_items, data, label) {
  let keep = []

  for(let j=0; j < search_items.length; j++) {
    let item = search_items[j]
    let should_keep = true
    let begin_date = new Date(item.time)
    let now_time = new Date()
    let drift = (now_time.getTime() - begin_date.getTime()) / 1000
    if (drift > timeout_on_search) {
      // We did not find the record, even after waiting for TIMEOUT_TO_SEARCH
      await record(system_name, label, false, drift, begin_date, now_time)
      console.log("Failed to receive:", item.id, item.time)
      should_keep = false
    }
    for(let i=0; i < data.events.length; i++) {
      let event = data.events[i]
      if(event.message.indexOf(item.id) > -1) {
        let end_date = new Date(Date.parse(event.received_at)) // also has generated_at timestamp.
        let recorded_drift = (end_date.getTime() - begin_date.getTime()) / 1000
        if(recorded_drift > time_to_failure) {
          // We found the record but it was received so late that we must mark it 
          // as a failure
          await record(system_name, label, false, drift, begin_date, end_date)
          console.log("Slow to receive (failure):", item.id, item.time)
        } else {
          await record(system_name, label, true, drift, begin_date, end_date)
        }
        should_keep = false
      }
    }
    if(should_keep) {
      keep.push(item)
    }
  }
  return keep
}

async function watch_papertrail_logs() {
  if(process.env.TEST_MODE) {
    console.log(`-- Looking through papertrail logs, before ${search_for_app_ids.length} ${search_for_http_ids.length}`)
  }
  console.log(`Searching papertrail logs...`)
  let data = await fetch('https://papertrailapp.com/api/v1/events/search.json?system_id=' + system_name, 'get', {"X-Papertrail-Token":papertrail_token}, null)
  if(!data || !data.events) {
    console.error("Malformed response from papertrail:", data);
    return;
  } else {
    search_for_app_ids = await search_for_record(search_for_app_ids, data, 'app_logs')
    search_for_http_ids = await search_for_record(search_for_http_ids, data, 'http_logs')
  }
  if(process.env.TEST_MODE) {
    console.log(`-- Looked through papertrail logs, after ${search_for_app_ids.length} ${search_for_http_ids.length}`)
  }
}

async function output_app_log_test() {
  try {
    let unique_id = Math.round(Math.random() * 100000000).toString()
    let begin_date = new Date()
    console.log("id", unique_id, "time:", begin_date)
    search_for_app_ids.push({"id":unique_id, "time":begin_date.getTime()})
  } catch(e) {
    console.error('output_app_log_test error', e)
  }
}

async function output_http_log_test() {
  try {
    let unique_id = Math.round(Math.random() * 100000000).toString()
    let begin_date = new Date()
    await fetch(`${system_uri}/samples/${unique_id}`, 'get', {}, null)
    search_for_http_ids.push({"id":`/samples/${unique_id}`, "time":begin_date.getTime()})
  } catch(e) {
    console.error('output_http_log_test error', e)
  }
}

async function begin_http_server() {
  const server = http.createServer((req, res) => {
    if(process.env.TEST_MODE) {
      console.log("<- " + req.url)
    }
    res.writeHead(200, { 'Content-Type': 'text/plain' })
    res.end('')
  })
  server.listen(port)
}

async function begin_log_tests() {
  while(true) {
    try {
      await output_app_log_test()
      await wait(1000)
    } catch (e) {
      console.error("ERROR:", e)
    }
  }
}


async function begin_http_tests() {
  while(true) {
    try {
      await output_http_log_test()
      await wait(5000)
    } catch (e) {
      console.error("ERROR:", e)
    }
  }
}

async function begin_watcher() {
  while(true) {
    try {
      await wait(5000)
      await watch_papertrail_logs()
    } catch (e) {
      console.error("ERROR:", e)
    }
  }
}

(async function() {
  begin_http_server()
  begin_http_tests()
  begin_log_tests()
  begin_watcher()
})().catch((e) => console.error)
