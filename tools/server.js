#!/usr/bin/env node

import * as child_process from 'child_process';
import * as fs from 'fs';
import * as http from 'http';
import * as path from 'path';
import * as process from 'process';

const mimeTypeMap = {
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
    ".otf":   "font/opentype",
    ".eot":   "application/vnd.ms-fontobject",
    ".woff":  "application/font-woff",
    ".woff2": "application/font-woff2"
};

let folder = ".";
let port = 8080;
let browse = false;
const redirects = [];
let indexPage = "index.html";
let notFoundPage = "";

const args = process.argv.slice(2);
while (args.length > 0) {
    const arg = args.shift();
    if ((arg === "--port" || arg === "-p") && args.length > 0 && !isNaN(args[0])) {
        port = Number(args.shift());
    } else if ((arg === "--index-page" || arg === "-i") && args.length > 0) {
        indexPage = args.shift();
    } else if ((arg === "--not-found-page" || arg === "-n") && args.length > 0) {
        notFoundPage = args.shift();
    } else if ((arg === "--redirect-map" || arg === "-r") && args.length > 0) {
        const data = fs.readFileSync(args.shift(), "utf-8");
        const lines = data.split(/\r\n?|\n/g);
        while (lines.length > 0) {
            const line = lines.shift();
            const match = line.match("([^ ]*) *([^ ]*)");
            if (match && match[1] && match[2]) {
                redirects.push({
                    source: match[1],
                    target: match[2]
                });
            }
        }
    } else if (arg === "--browse" || arg === "-b") {
        browse = true;
    } else if (!arg.startsWith("-")) {
        folder = arg;
    }
}

const server = http.createServer((request, response) => {
    const url = new URL(request.url, `http://${request.headers.host}`);
    const pathname = url.pathname;
    let location = `${folder}${pathname}`;
    let status = 0;
    let headers = {};
    let buffer = null;
    for (let i = 0; i < redirects.length; i++) {
        if (redirects[i].source === pathname) {
            status = 301;
            headers = { Location: redirects[i].target };
            break;
        }
    }
    if (status === 0) {
        if (fs.existsSync(location) && fs.statSync(location).isDirectory()) {
            if (location.endsWith("/")) {
                location += indexPage;
            } else {
                status = 302;
                headers = { Location: `${pathname}/` };
            }
        }
    }
    if (status === 0) {
        if (fs.existsSync(location) && !fs.statSync(location).isDirectory()) {
            status = 200;
        } else {
            status = 404;
            location = `${folder}/${notFoundPage}`;
        }
        if (fs.existsSync(location) && !fs.statSync(location).isDirectory()) {
            buffer = fs.readFileSync(location, "binary");
            headers["Content-Length"] = buffer.length;
            const extension = path.extname(location);
            const contentType = mimeTypeMap[extension];
            if (contentType) {
                headers["Content-Type"] = contentType;
            }
        }
    }
    console.log(`${status} ${request.method} ${request.url}`);
    response.writeHead(status, headers);
    if (request.method !== "HEAD") {
        if (status === 404 && buffer === null) {
            response.write(status.toString());
        } else if ((status === 200 || status === 404) && buffer !== null) {
            response.write(buffer, "binary");
        }
    }
    response.end();
});

server.listen(port, (error) => {
    if (error) {
        console.log("ERROR: ", error);
        return;
    }
    const url = `http://localhost:${port}`;
    console.log(`Serving '${folder}' at ${url}...`);
    if (browse) {
        let command = "xdg-open";
        switch (process.platform) {
            case "darwin": command = "open"; break;
            case "win32": command = 'start ""'; break;
            default: throw new Error(`Unsupported platform '${process.platform}.`);
        }
        child_process.exec(`${command} "${url.replace(/"/g, '\\"')}"`);
    }
});
