#!/usr/bin/env python

import codecs
import datetime
import dateutil.parser
import dateutil.tz
import json
import os
import re
import platform
import sys
import shutil

if sys.version_info[0] > 2:
    from urllib.parse import urlparse
    from urllib.parse import parse_qs
else:
    from urlparse import urlparse
    from urlparse import parse_qs

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
    with codecs.open(path, mode="r", encoding="utf-8") as open_file:
        return open_file.read()

def write_file(path, data):
    if not os.path.exists(os.path.dirname(path)):
        os.makedirs(os.path.dirname(path))
    with codecs.open(path, mode="w", encoding="utf-8") as open_file:
        open_file.write(data)

def format_date(date, format):
    if format == "atom":
        return date.astimezone(dateutil.tz.gettz("UTC")).isoformat("T").split("+")[0] + "Z"
    if format == "rss":
        return date.astimezone(dateutil.tz.gettz("UTC")).strftime("%a, %d %b %Y %H:%M:%S %z")
    if format == "user":
        return date.strftime("%b %d, %Y").replace(" 0", " ")
    return ""

def posts():
    folders = []
    for post in sorted(os.listdir("content/blog"), reverse=True):
        if os.path.isdir("content/blog/" + post) and os.path.exists("content/blog/" + post + "/index.html"):
            folders.append(post)
    return folders

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
    if os.path.exists(path) and not os.path.isdir(path):
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

def render_blog(folders, desitination, root, page):
    view = { "items": [] }
    count = 10
    while count > 0 and len(folders) > 0:
        folder = folders.pop(0)
        item = load_post("content/blog/" + folder + "/index.html")
        if item and (item["state"] == "post" or environment != "production"):
            item["url"] = "blog/" + folder + "/"
            if "date" in item:
                date = dateutil.parser.parse(item["date"])
                item["date"] = format_date(date, "user")
            content = item["content"]
            content = re.sub(r"\s\s", " ", content)
            truncated = truncate(content, 250)
            item["content"] = truncated
            item["more"] = truncated != content
            view["items"].append(item)
            count -= 1
    view["placeholder"] = []
    view["root"] = root
    if len(folders) > 0:
        page += 1
        location = "blog/page" + str(page) + ".html";
        view["placeholder"].append({ "url": root + location })
        file = destination + "/" + location
        data = render_blog(folders, destination, root, page)
        write_file(file, data)
    template = read_file("themes/" + theme + "/feed.html")
    return mustache(template, view, None)

def render_post(source, destination, root):
    if source.startswith("content/blog/") and source.endswith("/index.html"):
        item = load_post(source)
        if item:
            if not "author" in item:
                item["author"] = configuration["name"]
            if "date" in item:
                date = dateutil.parser.parse(item["date"])
                item["date"] = format_date(date, "user")
            if not "telemetry" in item:
                item["telemetry"] = ""
            if "telemetry" in configuration:
                item["telemetry"] = mustache(configuration["telemetry"], item, None)
            view = merge([ configuration, item ])
            view["root"] = root
            template = read_file("themes/" + theme + "/post.html")
            data = mustache(template, view, lambda name: read_file("themes/" + theme + "/" + name))
            write_file(destination, data)
            return True
    return False

def render_feed(source, destination):
    host = configuration["host"]
    format = os.path.splitext(source)[1].replace(".", "")
    url = host + "/blog/feed." + format
    count = 10
    feed = {
        "name": configuration["name"],
        "description": configuration["description"],
        "author": configuration["name"],
        "host": host,
        "url": url,
        "items": [] 
    }
    recent_found = False
    recent = datetime.datetime.now()
    folders = posts()
    while len(folders) > 0 and count > 0:
        folder = folders.pop(0)
        item = load_post("content/blog/" + folder + "/index.html")
        if item and (item["state"] == "post" or environment != "production"):
            item["url"] = host + "/blog/" + folder + "/"
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
            item["content"] = escape_html(truncate(item["content"], 10000))
            feed["items"].append(item)
            count -= 1
    feed["updated"] = format_date(recent, format)
    template = read_file(source)
    data = mustache(template, feed, None)
    write_file(destination, data)

def render_page(source, destination, root):
    if render_post(source, destination, root):
        return
    template = read_file(os.path.join("./", source))
    view = merge([ configuration ])
    view["root"] = root
    view["blog"] = lambda: render_blog(posts(), os.path.dirname(destination), root + "../", 0) + """<script type=\"text/javascript\">
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
"""
    view["pages"] = []
    for page in configuration["pages"]:
        location = os.path.dirname(source)
        target = mustache(page["url"], view, None)
        active = os.path.normpath(os.path.join(location, target)) == location
        if active or ("visible" in page and page["visible"]):
            view["pages"].append({"name": page["name"], "url": page["url"], "active": active })
    data = mustache(template, view, lambda name: read_file("themes/" + theme + "/" + name))
    write_file(destination, data)

def render_file(source, destination):
    shutil.copyfile(source, destination)

def render(source, destination, root):
    print(destination)
    extension = os.path.splitext(source)[1]
    if extension == ".rss" or extension == ".atom":
        render_feed(source, destination)
    elif extension == ".html":
        render_page(source, destination, root)
    else:
        render_file(source, destination)

def render_directory(source, destination, root):
    if not os.path.exists(destination):
        os.makedirs(destination)
    for item in os.listdir(source):
        if not item.startswith("."):
            if os.path.isdir(source + item):
                render_directory(source + item + "/", destination + item + "/", root + "../")
            else:
                render(source + item, destination + item, root)

def clean_directory(directory):
    if os.path.exists(directory) and os.path.isdir(directory):
        for item in os.listdir(directory):
            item = directory + "/" + item
            if os.path.isdir(item):
                shutil.rmtree(item)
            else:
                os.remove(item)

environment = os.getenv("ENVIRONMENT")
print("python " + platform.python_version() + " " + (environment if environment else ""))
with open("content.json") as configurationFile:
    configuration = json.load(configurationFile)
destination = "build"
theme = "default"
args = sys.argv[1:]
while len(args) > 0:
    arg = args.pop(0)
    if arg == "--theme" and len(args) > 0:
        theme = args.pop(0)
    else:
        destination = arg
clean_directory(destination)
render_directory("content/", destination + "/", "")
