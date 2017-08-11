#!/usr/bin/env node

var fs = require("fs");
var http = require("http");
var path = require("path");
var url = require("url");
var child_process = require('child_process');

var root = ".";
var port = 8080;
var browse = false;

var args = process.argv.slice(2)
while (args.length > 0) {
    var arg = args.shift();
    if (arg == "--browse" || arg == "-b") { 
        browse = true;
    }
    else if ((arg == "--port" || arg == "-p") && args.length > 0 && !isNaN(args[0])) {
        port = Number(args.shift());
    }
    else {
        root = arg;
    }
}

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

var server = http.createServer(function (request, response) {
    var pathname = url.parse(request.url, true).pathname;
    var location = path.join(root, pathname);
    var statusCode = 404;
    var headers = {};
    if (fs.existsSync(location) && fs.statSync(location).isDirectory()) {
        if (!pathname.endsWith("/")) {
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
        if (contentType && fs.existsSync(location)) {
            try {
                var size = fs.statSync(location).size;
                var buffer = new Buffer(size);
                var descriptor = fs.openSync(location, "r");
                fs.readSync(descriptor, buffer, 0, buffer.length, 0);
                fs.closeSync(descriptor);
                statusCode = 200;
                headers = {
                    "Content-Type": contentType,
                    "Content-Length": buffer.length
                };
            }
            catch (error) {
                buffer = null;
                console.log("ERROR: " + error);
            }
        }
    }
    console.log(statusCode + " " + request.method + " " + request.url + " ");
    response.writeHead(statusCode, headers);
    if (buffer && request.method !== "HEAD") {
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
        var command = "";
        switch (process.platform) {
            case "darwin": command = "open"; break;
            case 'win32': command = 'start ""'; break;
            default: command = "xdg-open"; break;
        }
        if (command) {
            child_process.exec(command + ' "' + url.replace(/"/g, '\\\"') + '"');
        }
    }
})