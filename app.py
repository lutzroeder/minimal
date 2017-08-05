#!/usr/bin/python

import codecs
import datetime
import json
import mimetypes
import os
import re
import platform
import sys
import dateutil.parser
import dateutil.tz

if sys.version_info[0] > 2:
    from urllib.parse import urlparse
    from urllib.parse import parse_qs
    from http.server import HTTPServer
    from http.server import BaseHTTPRequestHandler
else:
    from urlparse import urlparse
    from urlparse import parse_qs
    from BaseHTTPServer import HTTPServer
    from BaseHTTPServer import BaseHTTPRequestHandler

entity_map = {
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;",
    "'": "&#39;", "/": "&#x2F;", "`": "&#x60;", "=": "&#x3D;"
}

def escape_html(text):
    return "".join(entity_map.get(c, c) for c in text)

def merge(maps):
    target = {}
    for map in maps:
        target.update(map)
    return target

def mustache(template, view, partials):
    def replace_section(match):
        name = match.group(1)
        content = match.group(2)
        if name in view:
            section = view[name]
            if isinstance(section, list) and len(section) > 0:
                return "".join(mustache(content, merge([ view, item ]), partials) for item in section);
            if isinstance(section, bool) and section:
                return mustache(content, view, partials)
        return ""
    template = re.sub(r"{{#\s*([-_\/\.\w]+)\s*}}\s?([\s\S]*){{\/\1}}\s?", replace_section, template)
    def replace_partial(match):
        name = match.group(1)
        if callable(partials):
            return mustache(partials(name), view, partials)
        return match.group(0)
    template = re.sub(r"{{>\s*([-_/.\w]+)\s*}}", replace_partial, template)
    def replace(match):
        name = match.group(1)
        value = match.group(0)
        if name in view:
            value = view[name]
            if callable(value):
                value = value()
            return mustache(value, view, partials)
        return value
    template = re.sub(r"{{{\s*([-_/.\w]+)\s*}}}", replace, template)
    def replace_escape(match):
        name = match.group(1)
        value = match.group(0)
        if name in view:
            value = view[name]
            if callable(value):
                value = value()
            value = escape_html(value)
        return value
    template = re.sub(r"{{\s*([-_/.\w]+)\s*}}", replace_escape, template)
    return template

def read_file(path):
    with codecs.open(path, "r", "utf-8") as open_file:
        return open_file.read()

def host(request):
    if "host" in configuration:
        return configuration["host"]
    scheme = "http"
    value = request.headers.get("x-forwarded-proto")
    if value and len(value) > 0:
        scheme = value
    value = request.headers.get("x-forwarded-protocol")
    if value and len(value) > 0:
        scheme = value
    return scheme + "://" + request.headers.get("host")

def redirect(request, status, location):
    request.send_response(status)
    request.send_header("Location", location)
    request.end_headers()

def format_date(date, format):
    if format == "atom":
        return date.astimezone(dateutil.tz.gettz("UTC")).isoformat("T").split("+")[0] + "Z"
    if format == "rss":
        return date.astimezone(dateutil.tz.gettz("UTC")).strftime("%a, %d %b %Y %H:%M:%S %z")
    if format == "user":
        return date.strftime("%b %d, %Y").replace(" 0", " ")
    return ""

cache_data = {}

def cache(key, callback):
    if environment == "production":
        if not key in cache_data:
            cache_data[key] = callback()
        return cache_data[key]
    return callback()

path_cache = {}

def init_path_cache(directory):
    if environment == "production":
        for path in os.listdir(directory):
            if not path.startswith("."):
                path = directory + "/" + path
                if os.path.isdir(path):
                    path_cache[path + "/"] = True
                    init_path_cache(path)
                elif os.path.isfile(path):
                    path_cache[path] = True
            if directory == "." and path == ".well-known" and os.path.isdir(path):
                path_cache["./" + path + "/"] = True
                print("certificate")

def exists(path):
    if environment == "production":
        path = "./" + path
        return path in path_cache or (not path.endswith("/") and path + "/" in path_cache)
    return os.path.exists(path)

def isdir(path):
    if environment == "production":
        if path.endswith("/"):
            path = "./" + path
        else:
            path = "./" + path + "/"
        return path in path_cache
    return os.path.isdir(path)

def posts():
    def get_posts():
        folders = []
        for post in sorted(os.listdir("./blog"), reverse=True):
            if os.path.isdir("./blog/" + post) and os.path.exists("./blog/" + post + "/index.html"):
                folders.append(post)
        return folders
    return list(cache("blog:*", get_posts))

tag_regexp = re.compile(r"<(\w+)[^>]*>")
entity_regexp = re.compile(r"(#?[A-Za-z0-9]+;)")
break_regexp = re.compile(r" |<|&")
truncate_map = { "pre": True, "code": True, "img": True, "table": True, "style": True, "script": True, "h2": True, "h3": True }

def truncate(text, length):
    close_tags = {}
    ellipsis = ""
    count = 0
    index = 0
    while count < length and index < len(text):
        if text[index] == "<":
            if index in close_tags:
                index += len(close_tags.pop(index))
            else:
                match = tag_regexp.match(text[index:])
                if match:
                    tag = match.groups()[0].lower()
                    if tag in truncate_map and truncate_map[tag]:
                        break
                    index += match.end()
                    match = re.search("(</" + tag + "\\s*>)", text[index:], re.IGNORECASE)
                    if match:
                        close_tags[index + match.start()] = "</" + tag + ">"
                else:
                    index += 1
                    count += 1
        elif text[index] == "&":
            index += 1
            match = entity_regexp.match(text[index:])
            if match:
                index += match.end()
            count += 1
        else:
            if text[index] == " ":
                index += 1
                count += 1
            skip = len(text) - index
            match = break_regexp.search(text[index:])
            if match:
                skip = match.start()
            if count + skip > length:
                ellipsis = "&hellip;"
            if count + skip - 15 > length:
                skip = length - count
            index += skip
            count += skip
    output = [text[:index]]
    if len(ellipsis) > 0:
        output.append(ellipsis)
    for k in sorted(close_tags.keys()):
        output.append(close_tags[k])
    return "".join(output)

def load_post(path):
    if exists(path) and not isdir(path):
        data = read_file(path)
        item = {}
        content = []
        metadata = -1
        lines = re.split(r"\r\n?|\n", data)
        while len(lines) > 0:
            line = lines.pop(0)
            if line.startswith("---"):
                metadata += 1
            elif metadata == 0:
                index = line.find(":")
                if index >= 0:
                    name = line[0:index].strip()
                    value = line[index+1:].strip()
                    if value.startswith('"') and value.endswith('"'):
                        value = value[1:-1]
                    item[name] = value
            else:
                content.append(line)
        item["content"] = "\n".join(content)
        return item
    return None

def render_blog(folders, start):
    view = { "items": [] }
    count = 10
    index = 0
    while len(folders) > 0 and index < start + count:
        folder = folders.pop(0)
        item = load_post("blog/" + folder + "/index.html")
        if item and (item["state"] == "post" or environment != "production"):
            if index >= start:
                item["url"] = "/blog/" + folder + "/"
                if "date" in item:
                    date = dateutil.parser.parse(item["date"])
                    item["date"] = format_date(date, "user")
                content = item["content"]
                content = re.sub(r"\s\s", " ", content)
                truncated = truncate(content, 250)
                item["content"] = truncated
                item["more"] = truncated != content
                view["items"].append(item)
            index += 1
    view["placeholder"] = []
    if len(folders) > 0:
        view["placeholder"].append({ "url": "/blog/page" + str(index) + ".html" })
    template = read_file("./blog/feed.html")
    return mustache(template, view, None)

def write_string(request, content_type, data):
    encoded = data.encode("utf-8")
    request.send_response(200)
    request.send_header("Content-Type", content_type)
    request.send_header("Content-Length", len(encoded))
    request.end_headers()
    if request.command != "HEAD":
        request.wfile.write(encoded)

def root_handler(request):
    request.send_response(301)
    request.send_header("Location", "/")
    request.end_headers()

def feed_handler(request):
    pathname = urlparse(request.path).path.lower()
    filename = os.path.basename(pathname)
    format = os.path.splitext(filename)[1].replace(".", "")
    url = host(request) + pathname
    def render_feed():
        count = 10
        feed = {
            "name": configuration["name"],
            "description": configuration["description"],
            "author": configuration["name"],
            "host": host(request),
            "url": url,
            "items": [] 
        }
        recent_found = False
        recent = datetime.datetime.now()
        folders = posts()
        while len(folders) > 0 and count > 0:
            folder = folders.pop(0)
            item = load_post("blog/" + folder + "/index.html")
            if item and (item["state"] == "post" or environment != "production"):
                item["url"] = host(request) + "/blog/" + folder + "/"
                if not "author" in item or item["author"] == configuration["name"]:
                    item["author"] = False
                if "date" in item:
                    date = dateutil.parser.parse(item["date"])
                    updated = date
                    if "updated" in item:
                        updated = dateutil.parser.parse(item["updated"])
                    item["date"] = format_date(date, format)
                    item["updated"] = format_date(updated, format)
                    if not recent_found or recent < updated:
                        recent = updated
                        recent_found = True
                item["content"] = escape_html(truncate(item["content"], 10000));
                feed["items"].append(item)
                count -= 1
        feed["updated"] = format_date(recent, format)
        template = read_file("./blog/" + filename)
        return mustache(template, feed, None)
    data = cache("feed:" + url, render_feed)
    write_string(request, "application/" + format + "+xml", data)

def render_post(file, host):
    if file.startswith("blog/") and file.endswith("/index.html"):
        item = load_post(file)
        if item:
            if not "author" in item:
                item["author"] = configuration["name"]
            if "date" in item:
                date = dateutil.parser.parse(item["date"])
                item["date"] = format_date(date, "user")
            view = merge([ configuration, item ])
            view["/"] = "/"
            view["host"] = host;
            template = read_file("./blog/post.html")
            return mustache(template, view, lambda name: read_file(name))
    return ""

blog_regexp = re.compile("/blog/page(.*).html$")

def blog_handler(request):
    pathname = urlparse(request.path).path.lower()
    match = blog_regexp.match(pathname) 
    if match and match.group(1).isdigit():
        start = int(match.group(1))
        folders = posts()
        data = ""
        if start < len(folders):
            data = cache("default:" + pathname, lambda: render_blog(folders, start))
        write_string(request, "text/html", data)
        return
    root_handler(request)

def cert_handler(request):
    filename = urlparse(request.path).path
    if exists(".well-known/") and isdir(".well-known/"):
        if os.path.exists(filename) and os.path.isfile(filename):
            data = read_file(filename)
            return write_string(request, "text/plain; charset=utf-8", data)
            return
    request.send_response(404)
    request.end_headers()

def default_handler(request):
    pathname = urlparse(request.path).path.lower()
    if pathname.endswith("/index.html"):
        redirect(request, 301, "/" + pathname[0:len(pathname) - 11].lstrip("/"))
        return
    filename = pathname
    if pathname.endswith("/"):
        filename = os.path.join(pathname, "index.html")
    filename = filename.lstrip("/")
    if not exists(filename):
        redirect(request, 302, os.path.dirname(pathname))
        return
    if isdir(filename):
        redirect(request, 302, pathname + "/")
        return
    extension = os.path.splitext(filename)[1]
    content_type = mimetypes.types_map[extension]
    if content_type and content_type != "text/html":
        def content():
            with open(os.path.join("./", filename), "rb") as binary:
                return binary.read()
        buffer = cache("default:" + filename, content)
        request.send_response(200)
        request.send_header("Content-Type", content_type)
        request.send_header("Content-Length", len(buffer))
        request.send_header("Cache-Control", "private, max-age=0")
        request.send_header("Expires", -1)
        request.end_headers()
        if request.command != "HEAD":
            request.wfile.write(buffer)
        return
    def content():
        post = render_post(filename, host(request))
        if len(post) > 0:
            return post
        template = read_file(os.path.join("./", filename))
        view = merge([ configuration ])
        view["/"] = "/"
        view["host"] = host(request)
        view["blog"] = lambda: render_blog(posts(), 0)
        return mustache(template, view, lambda name: read_file(name))
    data = cache("default:" + filename, content)
    write_string(request, "text/html", data)

class Router(object):
    def __init__(self, configuration):
        self.routes = []
        if "redirects" in configuration:
            for redirect in configuration["redirects"]:
                self.get(redirect["pattern"], redirect["target"])
    def get(self, pattern, handler):
        self.route(pattern)["handlers"]["GET"] = handler
    def route(self, pattern):
        route = next((route for route in self.routes if route["pattern"] == pattern), None)
        if not route:
            route = {
                "pattern": pattern,
                "regexp": re.compile("^" + pattern.replace(".", "\.").replace("*", "(.*)") + "$"),
                "handlers": {}
            }
            self.routes.append(route)
        return route
    def handle(self, request):
        url = urlparse(request.path)
        for route in self.routes:
            if route["regexp"].match(url.path):
                method = request.command
                if method == "HEAD" and not "HEAD" in route["handlers"]:
                    method = "GET"
                handler = route["handlers"][method]
                if callable(handler): 
                    handler(request)
                else:
                    request.send_response(301)
                    request.send_header("Location", handler)
                    request.end_headers()
                return

class HTTPRequestHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        print(self.command + " " + self.path)
        router.handle(self)
    def do_HEAD(self):
        print(self.command + " " + self.path)
        router.handle(self)
    def log_message(self, format, *args):
        return

print("python " + platform.python_version())
with open("./app.json") as configurationFile:
    configuration = json.load(configurationFile)
environment = os.getenv("PYTHON_ENV")
print(environment)
init_path_cache(".")
router = Router(configuration)
router.get("/blog/feed.atom", feed_handler)
router.get("/blog/feed.rss", feed_handler)
router.get("/blog/page*.html", blog_handler)
router.get("/.well-known/acme-challenge/*", cert_handler)
router.get("/*", default_handler)
port = 8080
print("http://" + "localhost" + ":" + str(port))
server = HTTPServer(("localhost", port), HTTPRequestHandler)
server.serve_forever()
