#!/usr/bin/env node

"use strict";

var fs = require("fs");
var http = require("http");
var path = require("path");
var process = require("process");

var entityMap = {
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;", "/": "&#x2F;", "`": "&#x60;", "=": "&#x3D;"
};

function escapeHtml(text) {
    return text.replace(/[&<>"'`=\/]/g, function (char) {
        return entityMap[char];
    });
}

function merge() {
    var target = {};
    for (var i = 0; i < arguments.length; i++) {
        target = Object.assign(target, arguments[i]);
    }
    return target;
}

function mustache(template, view, partials) {
    template = template.replace(/{{#\s*([-_\/\.\w]+)\s*}}\s?([\s\S]*){{\/\1}}\s?/gm, function (match, name, content) {
        if (name in view) {
            var section = view[name];
            if (Array.isArray(section) && section.length > 0) {
                return section.map(item => mustache(content, merge(view, item), partials)).join("");
            }
            if (typeof(section) === "boolean" && section) {
                return mustache(content, view, partials);
            }
        }
        return "";
    });
    template = template.replace(/{{>\s*([-_\/\.\w]+)\s*}}/gm, function (match, name) {
        return mustache(typeof partials === "function" ? partials(name) : partials[name], view, partials);
    });
    template = template.replace(/{{{\s*([-_\/\.\w]+)\s*}}}/gm, function (match, name) {
        var value = view[name];
        return mustache(typeof value === "function" ? value() : value, view, partials);
    });
    template = template.replace(/{{\s*([-_\/\.\w]+)\s*}}/gm, function (match, name) {
        var value = view[name];
        return escapeHtml(typeof value === "function" ? value() : value);
    });
    return template;
}

function formatDate(date, format) {
    switch (format) {
        case "atom":
            return date.toISOString().replace(/\.[0-9]*Z/, "Z");
        case "rss":
            return date.toUTCString().replace(" GMT", " +0000");
        case "user":
            var months = [ "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec" ];
            return months[date.getMonth()] + " " + date.getDate() + ", " + date.getFullYear();
    }
    return "";
}

var truncateMap = { "pre": true, "code": true, "img": true, "table": true, "style": true, "script": true, "h2": true, "h3": true };

function truncate(text, length) {
    var closeTags = {};
    var ellipsis = "";
    var count = 0;
    var index = 0;
    while (count < length && index < text.length) {
        if (text[index] == '<') {
            if (index in closeTags) {
                var closeTagLength = closeTags[index].length;
                delete closeTags[index];
                index += closeTagLength;
            } 
            else {
                var match = text.substring(index).match("<(\\w+)[^>]*>");
                if (match) {
                    var tag = match[1].toLowerCase();
                    if (tag in truncateMap) {
                        break;
                    }
                    index += match[0].length;
                    var closeTagRegExp = new RegExp("(</" + tag + "\\s*>)", "i");
                    var end = text.substring(index).search(closeTagRegExp);
                    if (end != -1) {
                        closeTags[index + end] = "</" + tag + ">";
                    }
                }
                else {
                    index++;
                    count++;
                }
            }
        }
        else if (text[index] == "&") {
            index++;
            var entity = text.substring(index).match("(#?[A-Za-z0-9]+;)");
            if (entity) {
                index += entity[0].length;
            }
            count++;
        }
        else {
            if (text[index] == " ") {
                index++;
                count++;
            }
            var skip = text.substring(index).search(" |<|&");
            if (skip == -1) {
                skip = text.length - index;
            }
            if (count + skip > length) {
                ellipsis = "&hellip;";
            }
            if (count + skip - 15 > length) {
                skip = length - count;
            }
            index += skip;
            count += skip;
        }
    }
    var output = [ text.substring(0, index) ];
    if (ellipsis !== "") {
        output.push(ellipsis);
    }
    var keys = [];
    for (var key in closeTags) {
        keys.push(Number(key));
    }
    keys.sort().forEach(function (key) {
        output.push(closeTags[key]);
    });
    return output.join("");
}

function loadPost(file) {
    if (fs.existsSync(file) && !fs.statSync(file).isDirectory()) {
        var data = fs.readFileSync(file, "utf-8");
        if (data) {
            var item = {};
            var content = [];
            var metadata = -1;
            var lines = data.split(/\r\n?|\n/g);
            while (lines.length > 0) {
                var line = lines.shift();
                if (line.startsWith("---")) {
                    metadata++;
                }
                else if (metadata === 0) {
                    var index = line.indexOf(":");
                    if (index >= 0) {
                        var name = line.slice(0, index).trim();
                        var value = line.slice(index + 1).trim();
                        if (value.startsWith('"') && value.endsWith('"')) {
                            value = value.slice(1, -1);
                        }
                        item[name] = value;
                    }
                }
                else {
                    content.push(line);
                }
            }
            item["content"] = content.join("\n");
            return item;
        }
    }
    return null;
}

function posts() {
    return fs.readdirSync("content/blog/").filter(post => fs.statSync("content/blog/" + post).isDirectory() && fs.existsSync("content/blog/" + post + "/index.html")).sort().reverse();
}

function renderBlog(folders, destination, root, page) {
    var view = { "items": [] };
    var count = 10;
    while (count > 0 && folders.length > 0) {
        var folder = folders.shift();
        var item = loadPost("content/blog/" + folder + "/index.html");
        if (item && (item["state"] === "post" || environment !== "production")) {
            item["url"] = "blog/" + folder + "/";
            if ("date" in item) {
                var date = new Date(item["date"].split(/ \+| \-/)[0] + "Z");
                item["date"] = formatDate(date, "user");
            }
            var content = item["content"];
            content = content.replace(/\s\s/g, " ");
            var truncated = truncate(content, 250);
            item["content"] = truncated;
            item["more"] = truncated != content;
            view["items"].push(item);
            count--;
        }
    }
    view["placeholder"] = [];
    view["root"] = root;
    if (folders.length > 0) {
        page++;
        var location = "blog/page" + page.toString() + ".html";
        view["placeholder"].push({ "url": root + location });
        var file = destination + '/' + location;
        var data = renderBlog(folders, destination, root, page);
        fs.writeFileSync(file, data);
    }
    var template = fs.readFileSync("themes/" + theme + "/feed.html", "utf-8");
    return mustache(template, view, null);
}

function renderFeed(source, destination) {
    var host = configuration["host"];
    var format = path.extname(source).replace(".", "")
    var url = host + "/blog/feed." + format;
    var count = 10;
    var feed = {
        "name": configuration["name"],
        "description": configuration["description"],
        "author": configuration["name"],
        "host": host,
        "url": url,
        "items": [] 
    };
    var folders = posts();
    var recentFound = false;
    var recent = new Date();
    while (folders.length > 0 && count > 0) {
        var folder = folders.shift();
        var item = loadPost("content/blog/" + folder + "/index.html");
        if (item && (item["state"] === "post" || environment !== "production")) {
            item["url"] = host + "/blog/" + folder + "/"; 
            if (!item["author"] || item["author"] === configuration["name"]) {
                item["author"] = false;
            }
            if ("date" in item) {
                var date = new Date(item["date"]);
                var updated = date;
                if ("updated" in item) {
                    updated = new Date(item["updated"]);
                }
                item["date"] = formatDate(date, format);
                item["updated"] = formatDate(updated, format);
                if (!recentFound || recent < updated) {
                    recent = updated;
                    recentFound = true;
                }
            }
            item["content"] = escapeHtml(truncate(item["content"], 10000));
            feed["items"].push(item);
            count--;
        }
    }
    feed["updated"] = formatDate(recent, format);
    var template = fs.readFileSync(source, "utf-8");
    var data = mustache(template, feed, null);
    fs.writeFileSync(destination, data);
}

function renderPost(source, destination, root) {
    if (source.startsWith("content/blog/") && source.endsWith("/index.html")) {
        var item = loadPost(source);
        if (item) {
            if ("date" in item) {
                var date = new Date(item["date"].split(/ \+| \-/)[0] + "Z");
                item["date"] = formatDate(date, "user");
            }
            item["author"] = item["author"] || configuration["name"];
            var view = merge(configuration, item);
            view["root"] = root;
            var template = fs.readFileSync("themes/" + theme + "/post.html", "utf-8");
            var data = mustache(template, view, function(name) {
                return fs.readFileSync("themes/" + theme + "/" + name, "utf-8");
            });
            fs.writeFileSync(destination, data);
            return true;
        }
    }
    return false;
}

function renderPage(source, destination, root) {
    if (renderPost(source, destination, root)) {
        return;
    }
    var template = fs.readFileSync(source, "utf-8");
    var view = merge(configuration);
    view["root"] = root;
    view["blog"] = function() {
        return renderBlog(posts(), path.dirname(destination), root + '../', 0) + `<script type="text/javascript">
function updateStream() {
    var element = document.getElementById("stream");
    if (element) {
      var rect = element.getBoundingClientRect();
      var threshold = 0;
      if (rect.bottom > threshold && (window.innerHeight - rect.top) > threshold) {
        var url = element.getAttribute("title");
        var xmlHttp = new XMLHttpRequest();
        xmlHttp.open("GET", url, true);
        xmlHttp.onreadystatechange = function () {
            if (xmlHttp.readyState == 4 && xmlHttp.status == 200) {
                element.insertAdjacentHTML('beforebegin', xmlHttp.responseText);
                element.parentNode.removeChild(element);
                updateStream();
            }
        };
        xmlHttp.send(null);
      }
    }
}
updateStream();
window.addEventListener('scroll', function(e) {
    updateStream();
});
</script>
`
    };
    view["pages"] = [];
    configuration["pages"].forEach(function (page) {
        const location = path.dirname(source);
        const target = mustache(page["url"], view);
        const active = path.join(location, target) == location;
        if (active || page["visible"]) {
            view["pages"].push({ "name": page["name"], "url": page["url"], "active": active });
        }
    });
    var data = mustache(template, view, function(name) {
        return fs.readFileSync("themes/" + theme + "/" + name, "utf-8");
    });
    fs.writeFileSync(destination, data);
}

function renderFile(source, destination) {
    fs.createReadStream(source).pipe(fs.createWriteStream(destination));
}

function render(source, destination, root) {
    console.log(destination);
    var extension = path.extname(source);
    switch (extension) {
        case ".rss":
        case ".atom":
            renderFeed(source, destination);
            break;
        case ".html":
            renderPage(source, destination, root);
            break;
        default:
            renderFile(source, destination);
            break;
    }
}

function makeDirectory(directory) { 
    directory.split("/").reduce((current, folder) => {
        current += folder + "/";
        if (!fs.existsSync(current)) {
            fs.mkdirSync(current);
        }
        return current;
    }, '');
}

function renderDirectory(source, destination, root) {
    makeDirectory(destination);
    fs.readdirSync(source).forEach(function(item) {
        if (!item.startsWith(".")) {
            if (fs.statSync(source + item).isDirectory()) {
                renderDirectory(source + item + "/", destination + item + "/", root + '../');
            }
            else {
                render(source + item, destination + item, root);
            }
        }
    });
}

function cleanDirectory(directory) {
    if (fs.existsSync(directory) && fs.statSync(directory).isDirectory()) {
        fs.readdirSync(directory).forEach(function (item, index) {
            item = directory + "/" + item;
            if (fs.statSync(item).isDirectory()) {
                cleanDirectory(item)
                fs.rmdirSync(item);
            }
            else {
                fs.unlinkSync(item);
            }
        });
    }
}

var environment = process.env["ENVIRONMENT"];
console.log("node " + process.version + " " + environment);
var configuration = JSON.parse(fs.readFileSync("content.json", "utf-8"));
var destination = "build";
var theme = "default";
var args = process.argv.slice(2)
while (args.length > 0) {
    var arg = args.shift();
    if (arg == "--theme" && args.length > 0) {
        theme = args.shift();
    }
    else {
        destination = arg;
    }
}
cleanDirectory(destination);
renderDirectory("content/", destination + '/', '');
