#!/usr/bin/env node

var fs = require("fs");
var http = require("http");
var path = require("path");
var url = require("url");
var child_process = require('child_process');

var mimeTypeMap = {
    ".html": "text/html",
    ".js":    "text/javascript",
    ".css":   "text/css",
    ".png":   "image/png",
    ".gif":   "image/gif",
    ".jpg":   "image/jpeg",
    ".ico":   "image/x-icon",
    ".rss":   "application/rss+xml",
    ".atom":  "application/atom+xml",
    ".json":  "application/json",
    ".zip":   "application/zip",
    ".svg":   "image/svg+xml",
    ".ttf":   "font/truetype",
    ".woff":  "font/woff",
    ".otf":   "font/opentype",
    ".eot":   "application/vnd.ms-fontobject",
    ".woff":  "application/font-woff",
    ".woff2": "application/font-woff2"
};

var folder = ".";
var port = 8080;
var browse = false;
var redirects = [];
var indexPage = "index.html";
var notFoundPage = "";

var args = process.argv.slice(2)
while (args.length > 0) {
    var arg = args.shift();
    if ((arg == "--port" || arg == "-p") && args.length > 0 && !isNaN(args[0])) {
        port = Number(args.shift());
    }
    else if ((arg == "--index-page" || arg == "-i") && args.length > 0) {
        indexPage = args.shift();
    }
    else if ((arg == "--not-found-page" || arg == "-n") && args.length > 0) {
        notFoundPage = args.shift();
    }
    else if ((arg == "--redirect-map" || arg == "-r") && args.length > 0) {
        var data = fs.readFileSync(args.shift(), "utf-8");
        var lines = data.split(/\r\n?|\n/g);
        while (lines.length > 0) {
            var line = lines.shift();
            match = line.match("([^ ]*) *([^ ]*)");
            if (match && match[1] && match[2]) {
                redirects.push({
                    source: match[1],
                    target: match[2]
                });
            }
        }
    }
    else if (arg == "--browse" || arg == "-b") { 
        browse = true;
    }
    else if (!arg.startsWith("-")) {
        folder = arg;
    }
}

var server = http.createServer(function (request, response) {
    var pathname = url.parse(request.url, true).pathname;
    var location = folder + pathname;
    var statusCode = 0;
    var headers = {};
    var buffer = null;
    for (var i = 0; i < redirects.length; i++) {
        if (redirects[i].source == pathname) {
            statusCode = 301;
            headers = { "Location": redirects[i].target };
            break;
        }        
    }
    if (statusCode == 0) {
        if (fs.existsSync(location) && fs.statSync(location).isDirectory()) {
            if (location.endsWith("/")) {
                location += indexPage;
            }
            else {
                statusCode = 302;
                headers = { "Location": pathname + "/" };
            }
        }
    }
    if (statusCode == 0) {
        if (fs.existsSync(location) && !fs.statSync(location).isDirectory()) {
            statusCode = 200;
        }
        else {
            statusCode = 404
            location = folder + "/" + notFoundPage;
        }
        if (fs.existsSync(location) && !fs.statSync(location).isDirectory()) {
            buffer = fs.readFileSync(location, "binary");
            headers["Content-Length"] = buffer.length;
            var extension = path.extname(location);
            var contentType = mimeTypeMap[extension];
            if (contentType) {
                headers["Content-Type"] = contentType;
            }
        }
    }
    console.log(statusCode + " " + request.method + " " + request.url);
    response.writeHead(statusCode, headers);
    if (request.method !== "HEAD") {
        if (statusCode == 404 && buffer == null) {
            response.write(statusCode.toString());
        }
        else if ((statusCode == 200 || statusCode == 404) && buffer != null) {
            response.write(buffer, "binary");
        }
    }
    response.end();
})

server.listen(port, function(error) {  
    if (error) {
        console.log("ERROR: ", error);
        return;
    }
    var url = "http://localhost:" + port;
    console.log("Serving '" + folder + "' at " + url + "...");
    if (browse) {
        var command = "xdg-open";
        switch (process.platform) {
            case "darwin": command = "open"; break;
            case "win32": command = 'start ""'; break;
        }
        child_process.exec(command + ' "' + url.replace(/"/g, '\\\"') + '"');
    }
})
