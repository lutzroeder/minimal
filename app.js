#!/usr/bin/env node

var fs = require("fs");
var http = require("http");
var os = require("os");
var path = require("path");
var url = require("url");

console.log(process.title + " " + process.version);
var configuration = JSON.parse(fs.readFileSync("./app.json", "utf-8"));

function Router() {
    this.routes = [];
}

Router.prototype.route = function(path) {
    var route = this.routes.find(function (route) {
        return route.path === path;
    });
    if (!route)
    {
        route = { 
            path: path,
            regexp: (path instanceof RegExp) ? path : new RegExp("^" + path.replace("/*", "/(.*)") + "$", "i"),
            handlers: {}
        };
        this.routes.push(route);
    }
    return route;
}

Router.prototype.handle = function(request, response) {
    var pathname = path.normalize(url.parse(request.url, true).pathname);
    var routes = this.routes;
    var defaultHandler = this.defaultHandler;
    var index = 0;
    next();
    function next() {
        var route = routes[index++];
        if (route) {
            if (pathname.match(route.regexp) !== null) {
                var method = request.method.toUpperCase();
                if (method === "HEAD" && !route.handlers["HEAD"]) {
                    method = "GET";
                }
                var handler = route.handlers[method];
                if (handler) {
                    try {
                        handler(request, response, next);
                    }
                    catch (error) {
                        console.log(error);
                        next(error);
                    }
                }
                else {
                    next();
                }
            }
            else {
                next();
            }
        }
        else if (defaultHandler) {
            defaultHandler(request, response, function (request, response, next) {});
        }
    }
}

Router.prototype.updateHandler = function(handler) {
    if (typeof handler === "string") {
        var url = handler;
        handler = function (request, response, next) {
            response.writeHead(302, { "Location": url });
            response.end();
        };
    }
    return handler;
}

Router.prototype.get = function(path, handler) {
    this.route(path).handlers["GET"] = this.updateHandler(handler);
}

Router.prototype.head = function(path, handler) {
    this.route(path).handlers["HEAD"] = this.updateHandler(handler);
}

Router.prototype.default = function(handler) {
    this.defaultHandler = this.updateHandler(handler);
}

var router = new Router();

router.default("/");

router.get("/.git(/.*)?", "/");
router.get("/admin", "/");
router.get("/admin.cfg", "/");
router.get("/app.js", "/");
router.get("/app.json", "/");
router.get("/header.html", "/");
router.get("/meta.html", "/");
router.get("/package.json", "/");
router.get("/post.html", "/");
router.get("/web.config", "/");

// ATOM feed
router.get("/blog/atom.xml", function(request, response, next) {
    var host = (request.secure ? "https" : "http") + "://" + request.headers.host;
    var output = [];
    output.push("<?xml version='1.0' encoding='UTF-8'?>");
    output.push("<feed xmlns='http://www.w3.org/2005/Atom'>");
    output.push("<title>" + configuration["name"] + "</title>");
    output.push("<id>" + host + "/</id>");
    output.push("<icon>" + host + "/favicon.ico</icon>");
    output.push("<updated>" + new Date().toISOString() + "</updated>");
    output.push("<author><name>" + configuration["name"] + "</name></author>");
    output.push("<link rel='alternate' type='text/html' href='" + host + "/' />");
    output.push("<link rel='self' type='application/atom+xml' href='" + host + "/blog/atom.xml' />");
    fs.readdirSync("blog/").sort().reverse().forEach(function (file, index) {
        var domain = request.headers.host ? request.headers.host.split(":").shift() : "";
        var draft = domain == "localhost" || domain == "127.0.0.1";
        var entry = loadPost("blog/" + file);
        if (entry && (entry["state"] == "post" || draft)) {
            var url = host + "/blog/" + path.basename(file, ".html");;
            output.push("<entry>");
            output.push("<id>" + url + "</id>");
            if (entry["author"] && entry["author"] !== configuration["name"]) {
                output.push("<author><name>" + entry["author"] + "</name></author>");
            }
            var date = new Date(entry["date"]).toISOString();
            output.push("<published>" + date + "</published>");
            output.push("<updated>" + (entry["updated"] ? (new Date(entry["updated"]).toISOString()) : date) + "</updated>");
            output.push("<title type='text'>" + entry["title"] + "</title>")
            output.push("<content type='html'>" + escapeHtml(entry["content"]) + "</content>");
            output.push("<link rel='alternate' type='text/html' href='" + url + "' title='" + entry["title"] + "' />");
            output.push("</entry>")
        }
    });
    output.push("</feed>");
    var data = output.join("\n");
    response.writeHead(200, {
        "Content-Type" : "application/atom+xml",
        "Content-Length" : Buffer.byteLength(data) });
    if (request.method !== "HEAD") {
        response.write(data);
    }
    response.end();
});

// Render specific HTML blog post
router.get("/blog/*", function (request, response, next) {
    var pathname = path.normalize(url.parse(request.url, true).pathname.toLowerCase());
    var localPath = pathname.replace(/^\/?/, "") + ".html";
    var entry = loadPost(localPath);
    if (entry) {
        var date = new Date(entry["date"]);
        entry["date"] = date.toLocaleDateString("en-US", { month: "short"}) + " " + date.getDate() + ", " + date.getFullYear();
        entry["author"] = entry["author"] ? entry["author"] : configuration["name"];

        var context = Object.assign(configuration, entry);
        var template = fs.readFileSync("post.html", "utf-8");
        var data = mustache(template, context, function(name) {
            return fs.readFileSync(name, "utf-8");
        });

        response.writeHead(200, { 
            "Content-Type" : "text/html",
            "Content-Length" : Buffer.byteLength(data) });
        if (request.method !== "HEAD") {
            response.write(data);
        }
        response.end();
    }
    else {
        response.writeHead(302, { "Location": "/" });
        response.end();
    }
});

// Handle "Let's Encrypt" challenge
router.get("/.well-known/acme-challenge/*", function (request, response, next) {
    var pathname = path.normalize(url.parse(request.url, true).pathname);
    var localPath = pathname.replace(/^\/?/, "");
    if (fs.existsSync(localPath) && fs.statSync(localPath).isFile) {
        var data = fs.readFileSync(localPath, "utf-8");
        response.writeHead(200, { 
            "Content-Type" : "text/plain; charset=utf-8",
            "Content-Length" : Buffer.byteLength(data) });
        response.write(data);
        response.end();
    } 
    else {
        response.writeHead(302, { "Location": "/" });
        response.end();
    }
});

var mimeTypeMap = {
    ".js":   "text/javascript",
    ".css":  "text/css",
    ".png":  "image/png",
    ".gif":  "image/gif",
    ".jpg":  "image/jpeg",
    ".ico":  "image/x-icon",
    ".zip":  "application/zip",
    ".json": "application/json"
};

router.get("/*", function (request, response, next) {
    var pathname = path.normalize(url.parse(request.url, true).pathname.toLowerCase());
    if (pathname.endsWith("/index.html"))
    {
        response.writeHead(301, { "Location": "/" + pathname.substring(0, pathname.length - 11).replace(/^\/?/, "") });
        response.end();
    }
    else {
        var localPath = (pathname.endsWith("/") ? path.join(pathname, "index.html") : pathname).replace(/^\/?/, "");
        var contentType = mimeTypeMap[path.extname(localPath)];
        if (contentType) {
            // Handle binary files
            fs.stat(localPath, function (error, stats) {
                if (error) {
                    response.writeHead(404, { "Content-Type": contentType });
                    response.end();
                }
                else if (stats.isDirectory()) {
                    response.writeHead(302, { "Location": pathname + "/" });
                    response.end();
                }
                else {
                    var stream = fs.createReadStream(localPath);
                    stream.on("error", function () {
                        response.writeHead(404, { "Content-Type": contentType });
                        response.end();
                    });
                    stream.on("open", function () {
                        response.writeHead(200, { 
                            "Content-Type" : contentType,
                            "Content-Length" : stats.size });
                        if (request.method === "HEAD") {
                            response.end();
                        } 
                        else {
                            stream.pipe(response);
                        }
                    });
                }
            });
        }
        else {
            // Handle HTML files
            fs.stat(localPath, function (error, stats) {
                if (error) {
                    if (localPath !== "index.html") {
                        response.writeHead(302, { "Location": path.dirname(pathname) });
                        response.end();
                    }
                    else {
                        next();
                    }
                }
                else if (stats.isDirectory() || path.extname(localPath) != ".html") {
                    response.writeHead(302, { "Location": pathname + "/" });
                    response.end();
                }
                else {
                    var template = fs.readFileSync(localPath, "utf-8");
                    var context = Object.assign({ }, configuration);
                    context["feed"] = context["feed"] ? context["feed"] : function() {
                        return (request.secure ? "https" : "http") + "://" + request.headers.host + "/blog/atom.xml";
                    };
                    context["blog"] = function() {
                        var domain = request.headers.host ? request.headers.host.split(":").shift() : "";
                        var draft = domain == "localhost" || domain == "127.0.0.1";
                        return renderBlog(draft);
                    };
                    context["social"] = function() { 
                        return configuration["links"].map(function (link) { 
                            return "<a class='icon' target='_blank' href='" + link["url"] + "' title='" + link["name"] + "'><span class='symbol'>" + link["symbol"] + "</span></a>";
                        }).join("\n");
                    };
                    context["tabs"] = function() { 
                        return configuration["pages"].map(function (page) {
                            return "<li class='tab'><a href='" + page["url"] + "'>" + page["name"] + "</a></li>";
                        }).join("\n");
                    };
                    var data = mustache(template, context, function(name) {
                        return fs.readFileSync(path.join(path.dirname(localPath), name), "utf-8");
                    });
                    response.writeHead(200, { 
                        "Content-Type" : "text/html",
                        "Content-Length" : Buffer.byteLength(data) });
                    if (request.method !== "HEAD") {
                        response.write(data);
                    }
                    response.end();
                }
            });
        }
    }
});

var entityMap = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
    '/': '&#x2F;',
    '`': '&#x60;',
    '=': '&#x3D;'
};

function escapeHtml(text) {
    return text.replace(/[&<>"'`=\/]/g, function (char) {
        return entityMap[char];
    });
}

function mustache(template, context, partials) {
    template = template.replace(/\{\{>\s*([-_\/\.\w]+)\s*\}\}/gm, function (match, name) {
        return typeof partials === "function" ? partials(name) : partials[name];
    });
    template = template.replace(/\{\{\{\s*([-_\/\.\w]+)\s*\}\}\}/gm, function (match, name) {
        var value = context[name];
        if (typeof value === "function") {
            value = value();
        }
        return value;
    });
    template = template.replace(/\{\{\s*([-_\/\.\w]+)\s*\}\}/gm, function (match, name) {
        var value = context[name];
        if (typeof value === "function") {
            value = value();
        }
        return escapeHtml(value);
    });
    return template;
}

function renderBlog(draft) {
    var output = [];
    fs.readdirSync("blog/").sort().reverse().forEach(function (file, index) {
        var entry = loadPost("blog/" + file);
        if (entry && (draft || entry["state"] === "post")) {
            entry["id"] = path.basename(file, ".html");
            var url = "/blog/" + entry["id"];
            var date = new Date(entry["date"]);
            entry["date"] = date.toLocaleDateString("en-US", { month: "short"}) + " " + date.getDate() + ", " + date.getFullYear();
            var post = [];
            post.push("<div class='item'>");
            post.push("<div class='date'>" + entry["date"] + "</div>");
            post.push("<h1><a href='" + url + "'>" + entry["title"] + "</a></h1>");
            var content = entry["content"];
            content = content.replace(/\s\s/g, " ");
            var truncated = truncateHtml(content, 320);
            post.push("<p>" + truncated + "</p>");
            if (truncated != content) {
                post.push("<div class='more'><a href='" + url + "'>" + "Read more&hellip;" + "</a></div>");
            }
            post.push("</div>");
            output.push(post.join(""));
        }
    });
    return output.join("")
}

function loadPost(file) {
    if (fs.existsSync(file) && fs.statSync(file).isFile) {
        var data = fs.readFileSync(file, "utf-8");
        if (data) {
            var entry = {};
            var content = [];
            var lines = data.split(/\r\n?|\n/g); // newline
            var line = lines.shift();
            if (line && line.startsWith("---")) {
                while (true) {
                    line = lines.shift();
                    if (!line || line.startsWith("---")) {
                        break;
                    }
                    var index = line.indexOf(":");
                    if (index > -1) {
                        var name = line.slice(0, index).trim();
                        var value = line.slice(index + 1).trim();
                        if (value.startsWith('"') && value.endsWith('"')) {
                            value = value.slice(1, -1);
                        }
                        entry[name] = value;
                    }
                }
            }
            else {
                content.append(line);
            }
            content = content.concat(lines);
            entry["content"] = content.join('\n');
            return entry;
        }
    }
    return null;
}

function truncateHtml(text, length) {
    var entityEndRegEx = /(\w+;)/g;
    var position = 0;
    var index = 0;
    var pendingCloseTags = {};
    while (position < length && index < text.length) {
        if (text[index] == '<') {
            if (index in pendingCloseTags) {
                var skip = pendingCloseTags[index].length;
                delete pendingCloseTags[index];
                index += skip;
            }
            else {
                index++;
                var match = text.substring(index).match(/(\w+)[^>]*>/);
                if (match) {
                    index--;
                    var tag = match[1];
                    if (tag == "pre" || tag == "code") {
                        break;
                    }
                    index += match[0].length;
                    match = text.substring(index).match(new RegExp("</" + tag + "[^>]*>"));
                    if (match) {
                        pendingCloseTags[index + match.index] = match[0];
                    }
                }
                else {
                    position++;
                }
            }
        }
        else if (text[index] == '&') {
            index += 1;
            match = entityEndRegEx.match(text.substring(index));
            if (match) {
                index += match.end();
            }
            position += 1;
        }
        else {
            var next = text.substring(index, length);
            var skip = next.indexOf('<');
            if (skip == -1) {
                skip = next.indexOf('&');
            }
            if (skip == -1) {
                skip = index + length;
            }
            var delta = Math.min(skip, length - position, text.length - index);
            position += delta;
            index += delta;
        }

    }
    var output = [ text.substring(0, index) ];
    if (position == length) {
        output.push('&hellip;');
    }
    var keys = [];
    for (var key in pendingCloseTags) {
        keys.push(Number(key));
    }
    keys.sort();
    keys.map(function (key) { return pendingCloseTags[key]; });
    return output.join("");
}

var server = http.createServer(function (request, response) {
    console.log(request.method + " " + request.url);
    router.handle(request, response);
});
var port = process.env.PORT || 8080;
server.listen(port, function() {
    console.log('http://localhost:' + port);
});
