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
    from http.server import HTTPServer
    from http.server import BaseHTTPRequestHandler
else:
    from urlparse import urlparse
    from BaseHTTPServer import HTTPServer
    from BaseHTTPServer import BaseHTTPRequestHandler

entity_map = {
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;",
    "'": "&#39;", "/": "&#x2F;", "`": "&#x60;", "=": "&#x3D;"
}

def escape_html(text):
    return "".join(entity_map.get(c, c) for c in text)

def mustache(template, context, partials):
    def replace_partial(match):
        name = match.group(1)
        value = match.group(0)
        if callable(partials):
            value = partials(name)
        return value
    template = re.sub(r"\{\{>\s*([-_/.\w]+)\s*\}\}", replace_partial, template)
    def replace(match):
        name = match.group(1)
        value = match.group(0)
        if name in context:
            value = context[name]
            if callable(value):
                value = value()
        return value
    template = re.sub(r"\{\{\{\s*([-_/.\w]+)\s*\}\}\}", replace, template)
    def replace_escape(match):
        name = match.group(1)
        value = match.group(0)
        if name in context:
            value = context[name]
            if callable(value):
                value = value()
            value = escape_html(value)
        return value
    template = re.sub(r"\{\{\s*([-_/.\w]+)\s*\}\}", replace_escape, template)
    return template

def read_file(path):
    with codecs.open(path, "r", "utf-8") as open_file:
        return open_file.read()

def path_join(root, path):
    if root == "./" and path.startswith("/"):
        return "." + path
    return os.path.join(root, path)

def scheme(request):
    value = request.headers.get("x-forwarded-proto")
    if value and len(value) > 0:
        return value
    value = request.headers.get("x-forwarded-protocol")
    if value and len(value) > 0:
        return value
    return "http"

def redirect(request, status, location):
    request.send_response(status)
    request.send_header("Location", location)
    request.end_headers()

def format_date(date):
    return date.astimezone(dateutil.tz.gettz("UTC")).isoformat("T").split("+")[0] + "Z"

def format_user_date(text):
    date = dateutil.parser.parse(text)
    return date.strftime("%b %-d, %Y").replace(" 0", " ")

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
        files = []
        for filename in sorted(os.listdir("./blog"), reverse=True):
            if os.path.splitext(filename)[1] == ".html":
                files.append(filename)
        return files
    return list(cache("blog:files", get_posts))

tag_regexp = re.compile(r"<(\w+)[^>]*>")
entity_regexp = re.compile(r"(#?[A-Za-z0-9]+;)")
break_regexp = re.compile(r" |<|&")
truncate_map = { "pre": True, "code": True, "img": True, "table": True, "style": True, "script": True }

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
    if os.path.isfile(path) and os.path.exists(path):
        data = read_file(path)
        entry = {}
        content = []
        metadata = -1
        lines = re.split(r"\r\n?|\n", data)
        while len(lines) > 0:
            line = lines.pop(0)
            if line.startswith("---"):
                metadata += 1
            else:
                if metadata == 0:
                    index = line.find(":")
                    if index >= 0:
                        name = line[0:index].strip()
                        value = line[index+1:].strip()
                        if value.startswith('"') and value.endswith('"'):
                            value = value[1:-1]
                        entry[name] = value
                else:
                    content.append(line)
        entry["content"] = "\n".join(content)
        return entry
    return None

def render_blog(files, start):
    output = []
    length = 10
    index = 0
    while len(files) > 0 and index < start + length:
        filename = files.pop(0)
        entry = load_post("./blog/" + filename)
        if entry and (entry["state"] == "post" or environment != "production"):
            if index >= start:
                location = "/blog/" + os.path.splitext(filename)[0]
                if "date" in entry:
                    entry["date"] = format_user_date(entry["date"])
                post = []
                post.append("<div class='item'>")
                post.append("<div class='date'>" + entry["date"] + "</div>")
                post.append("<h1><a href='" + location + "'>" + entry["title"] + "</a></h1>")
                post.append("<div class='content'>")
                content = entry["content"]
                content = re.sub(r"\s\s", " ", content)
                truncated = truncate(content, 250)
                post.append(truncated)
                post.append("</div>")
                if truncated != content:
                    post.append("<div class='more'><a href='" + location + "'>" + \
                        "Read more&hellip;" + "</a></div>")
                post.append("</div>")
                output.append("\n".join(post) + "\n")
            index += 1
    if len(files) > 0:
        template = read_file("./stream.html")
        context = {}
        context["url"] = "/blog?id=" + str(index)
        data = mustache(template, context, None)
        output.append(data)
    return "\n".join(output)

def write_string(request, content_type, data):
    encoded = data.encode("utf-8")
    request.send_response(200)
    request.send_header("Content-Type", content_type)
    request.send_header("Content-Length", len(encoded))
    request.end_headers()
    if request.command != "HEAD":
        request.wfile.write(encoded)

def atom_handler(request):
    host = scheme(request) + "://" + request.headers.get("host")
    def render_feed():
        count = 10
        output = []
        output.append("<?xml version='1.0' encoding='UTF-8'?>")
        output.append("<feed xmlns='http://www.w3.org/2005/Atom'>")
        output.append("<title>" + configuration["name"] + "</title>")
        output.append("<id>" + host + "/</id>")
        output.append("<icon>" + host + "/favicon.ico</icon>")
        index = len(output)
        recent = ""
        output.append("")
        output.append("<author><name>" + configuration["name"] + "</name></author>")
        output.append("<link rel='alternate' type='text/html' href='" + host + "/' />")
        output.append("<link rel='self' type='application/atom+xml' href='" + host + "/blog/atom.xml' />")
        files = posts()
        while len(files) > 0 and count > 0:
            filename = files.pop(0)
            entry = load_post("blog/" + filename)
            if entry and (entry["state"] == "post" or environment != "production"):
                url = host + "/blog/" + os.path.splitext(filename)[0]
                output.append("<entry>")
                output.append("<id>" + url + "</id>")
                if "author" in entry and entry["author"] != configuration["name"]:
                    output.append("<author><name>" + entry["author"] + "</name></author>")
                date = ""
                if "date" in entry:
                    date = format_date(dateutil.parser.parse(entry["date"]))
                output.append("<published>" + date + "</published>")
                updated = date
                if "updated" in entry:
                    updated = format_date(dateutil.parser.parse(entry["updated"]))
                output.append("<updated>" + updated + "</updated>")
                if len(recent) == 0 or recent < updated:
                    recent = updated
                output.append("<title type='text'>" + entry["title"] + "</title>")
                content = escape_html(truncate(entry["content"], 10000))
                output.append("<content type='html'>" + content + "</content>")
                output.append("<link rel='alternate' type='text/html' href='" + url + "' title='" + entry["title"] + "' />")
                output.append("</entry>")
                count -= 1
        if len(recent) == 0:
            recent = format_date(datetime.datetime.now())
        output[index] = "<updated>" + recent + "</updated>"
        output.append("</feed>")
        return "\n".join(output)
    data = cache("atom:" + host + "/blog/atom.xml", render_feed)
    write_string(request, "application/atom+xml", data)

def post_handler(request):
    url = urlparse(request.path)
    filename = os.path.abspath(url.path).lstrip("/").lower()
    def render_post():
        entry = load_post(filename + ".html")
        if entry:
            context = configuration.copy()
            context.update(entry)
            if not "author" in context:
                context["author"] = context["name"]
            if "date" in entry:
                context["date"] = format_user_date(entry["date"])
            template = read_file("./post.html")
            return mustache(template, context, lambda name: read_file(path_join("./", name)))
        return ""
    data = cache("post:"+ filename, render_post)
    if len(data) > 0:
        write_string(request, "text/html", data)
    else:
        extension = os.path.splitext(filename)
        if extension in mimetypes.types_map:
            default_handler(request)
        else:
            root_handler(request)

def blog_handler(request):
    url = urlparse(request.path)
    query = urlparse.parse_qs(url.query)
    if "id" in query:
        start = int(query["id"][0])
        key = "/blog?id=" + query["id"][0]
        files = posts()
        data = ""
        if start < len(files):
            data = cache("blog:" + key, lambda: render_blog(files, start))
        write_string(request, "text/html", data)
    else:
        root_handler(request)

def cert_handler(request):
    url = urlparse(request.path)
    filename = os.path.abspath(url.path)
    found = False
    if exists(".well-known/") and isdir(".well-known/"):
        if os.path.exists(filename) and os.path.isfile(filename):
            data = read_file(filename)
            request.send_response(200)
            request.send_header("Content-Type", "text/plain; charset=utf-8")
            request.send_header("Content-Length", len(data))
            request.end_headers()
            if request.command != "HEAD":
                request.wfile.write(data)
            found = True
    if not found:
        request.send_response(404)
        request.end_headers()

def default_handler(request):
    url = urlparse(request.path)
    pathname = os.path.abspath(url.path).lower()
    if pathname != "/" and url.path.endswith("/"):
        pathname += "/"
    if pathname.endswith("/index.html"):
        redirect(request, 301, "/" + pathname[0:len(pathname) - 11].lstrip("/"))
    else:
        filename = pathname
        if pathname.endswith("/"):
            filename = os.path.join(pathname, "index.html")
        filename = filename.lstrip("/")
        if not exists(filename):
            redirect(request, 302, os.path.dirname(pathname))
        elif isdir(filename):
            redirect(request, 302, pathname + "/")
        else:
            extension = os.path.splitext(filename)[1]
            content_type = mimetypes.types_map[extension]
            if content_type and content_type != "text/html":
                def content():
                    with open(os.path.join("./", filename), "rb") as binary:
                        return binary.read()
                data = cache("default:" + filename, content)
                request.send_response(200)
                request.send_header("Content-Type", content_type)
                request.send_header("Content-Length", len(data))
                request.send_header("Cache-Control", "private, max-age=0")
                request.send_header("Expires", -1)
                request.end_headers()
                if request.command != "HEAD":
                    request.wfile.write(data)
            else:
                def content():
                    template = read_file(os.path.join("./", filename))
                    context = configuration.copy()
                    context["feed"] = lambda: configuration["feed"] if \
                        ("feed" in configuration and len(configuration["feed"]) > 0) else \
                        scheme(request) + "://" + request.headers.get("host") + "/blog/atom.xml"
                    context["blog"] = lambda: render_blog(posts(), 0)
                    context["links"] = lambda: "\n".join( \
                        "<a class='icon' target='_blank' href='" + link["url"] + "' title='" + link["name"] + "'><span class='symbol'>" + link["symbol"] + "</span></a>" \
                        for link in configuration["links"])
                    context["tabs"] = lambda: "\n".join( \
                        "<li class='tab'><a href='" + page["url"] + "'>" + page["name"] + "</a></li>" \
                        for page in configuration["pages"]) 
                    return mustache(template, context, lambda name: read_file(path_join("./", name)))
                data = cache("default:" + filename, content)
                write_string(request, "text/html", data)

def root_handler(request):
    request.send_response(301)
    request.send_header("Location", "/")
    request.end_headers()

class Router(object):
    def __init__(self):
        self.routes = []
    def get(self, path, handler):
        self.route(path)["handlers"]["GET"] = handler
    def route(self, path):
        route = next((route for route in self.routes if route["path"] == path), None)
        if not route:
            route = {
                "path": path,
                "regexp": re.compile("^" + path.replace("*", "(.*)") + "$"),
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
                if handler:
                    handler(request)
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
router = Router()
router.get("/.git*", root_handler)
router.get("/admin*", root_handler)
router.get("/app", root_handler)
router.get("/package.json", root_handler)
router.get("/*.css", root_handler)
router.get("/*.html", root_handler)
router.get("/blog/atom.xml", atom_handler)
router.get("/blog/*", post_handler)
router.get("/blog", blog_handler)
router.get("/.well-known/acme-challenge/*", cert_handler)
router.get("/*", default_handler)
port = 8080
print("http://localhost:" + str(port))
server = HTTPServer(("", port), HTTPRequestHandler)
server.serve_forever()
