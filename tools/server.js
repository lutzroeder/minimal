#!/usr/bin/env node

var fs = require("fs");
var http = require("http");
var path = require("path");
var url = require("url");
var child_process = require('child_process');

var mimeTypeMap = {
    ".html": "text/html",
    ".js":   "text/javascript",
    ".css":  "text/css",
    ".png":  "image/png",
    ".gif":  "image/gif",
    ".jpg":  "image/jpeg",
    ".ico":  "image/x-icon",
    ".rss":  "application/rss+xml",
    ".atom": "application/atom+xml",
    ".json": "application/json",
    ".zip":  "application/zip"
};

var root = ".";
var port = 8080;
var browse = false;

var args = process.argv.slice(2)
while (args.length > 0) {
    var arg = args.shift();
    if ((arg == "--port" || arg == "-p") && args.length > 0 && !isNaN(args[0])) {
        port = Number(args.shift());
    }
    else if (arg == "--browse" || arg == "-b") { 
        browse = true;
    }
    else {
        root = arg;
    }
}

var server = http.createServer(function (request, response) {
    var pathname = url.parse(request.url, true).pathname;
    var location = root + pathname;
    var statusCode = 404;
    var headers = {};
    if (fs.existsSync(location) && fs.statSync(location).isDirectory()) {
        if (!location.endsWith("/")) {
            statusCode = 302;
            headers = { "Location": pathname + "/" };
        }
        else {
            location += "index.html";
        }
    }
    var buffer = null;
    if (fs.existsSync(location) && !fs.statSync(location).isDirectory()) {
        var extension = path.extname(location);
        var contentType = mimeTypeMap[extension];
        if (contentType) {
            buffer = fs.readFileSync(location, "binary");
            statusCode = 200;
            headers = {
                "Content-Type": contentType,
                "Content-Length": buffer.length
            };
        }
    }
    console.log(statusCode + " " + request.method + " " + request.url);
    response.writeHead(statusCode, headers);
    if (statusCode != 200) {
        response.write(statusCode.toString());
    }
    else if (request.method !== "HEAD") {
        response.write(buffer, "binary");
    }
    response.end();
})

server.listen(port, function(error) {  
    if (error) {
        console.log("ERROR: ", error);
        return;
    }
    var url = "http://localhost:" + port;
    console.log("Serving '" + root + "' at " + url + "...");
    if (browse) {
        var command = "xdg-open";
        switch (process.platform) {
            case "darwin": command = "open"; break;
            case "win32": command = 'start ""'; break;
        }
        child_process.exec(command + ' "' + url.replace(/"/g, '\\\"') + '"');
    }
})