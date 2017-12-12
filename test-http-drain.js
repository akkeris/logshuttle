const http = require('http');

const server = http.createServer((req, res) => {
	let body = new Buffer(0)
	req.on('data', (chunk) => {
		body = Buffer.concat([body,chunk])
	});
	req.on('end', () => {
		console.log(req.method, req.url)
		console.log(Object.keys(req.headers).map((x) => { return '  ' + x + ':' + req.headers[x]; }).join('\n'));
		console.log(body.toString('utf8'));
		console.log()
		res.writeHead(200, {'Content-Type': 'text/plain'});
		res.end('');
	});
});

server.listen(8080);