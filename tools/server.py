#!/usr/bin/env python

import codecs
import mimetypes
import os
import re
import sys
import threading
import webbrowser

if sys.version_info[0] > 2:
    from urllib.parse import urlparse
    from http.server import HTTPServer
    from http.server import BaseHTTPRequestHandler
else:
    from urlparse import urlparse
    from BaseHTTPServer import HTTPServer
    from BaseHTTPServer import BaseHTTPRequestHandler

folder = "."
port = 8080
browse = False
redirects = []
index_page = "index.html"
not_found_page = ""

args = sys.argv[1:]
while len(args) > 0:
    arg = args.pop(0)
    if (arg == "--port" or arg == "-p") and len(args) > 0 and args[0].isdigit(): 
        port = int(args.pop(0))
    if (arg == "--index-page" or arg == "-i") and len(args) > 0: 
        index_page = args.pop(0)
    if (arg == "--not-found-page" or arg == "-e") and len(args) > 0: 
        not_found_page = args.pop(0)
    elif (arg == "--redirect-map" or arg == "-r") and len(args) > 0:
        with codecs.open(args.pop(0), "r", "utf-8") as open_file:
            data = open_file.read()
            lines = re.split(r"\r\n?|\n", data)
            while len(lines) > 0:
                line = lines.pop(0)
                match = re.compile("([^ ]*) *([^ ]*)").match(line)
                if match and len(match.groups()[0]) > 0 and len(match.groups()[1]) > 0:
                    redirects.append({ 
                        "source": match.groups()[0], 
                        "target": match.groups()[1] })
    elif arg == "--browse" or arg == "-b":
        browse = True
    elif not arg.startswith("-"):
        folder = arg

class HTTPRequestHandler(BaseHTTPRequestHandler):
    def handler(self):
        pathname = urlparse(self.path).path
        location = folder + pathname;
        status_code = 0
        headers = {}
        buffer = None
        for redirect in redirects:
            if redirect["source"] == pathname:
                status_code = 301
                headers = { "Location": redirect["target"] }
        if status_code == 0:
            if os.path.exists(location) and os.path.isdir(location):
                if location.endswith("/"):
                    location += index_page
                else:
                    status_code = 302
                    headers = { "Location": pathname + "/" }
        if status_code == 0:
            if os.path.exists(location) and not os.path.isdir(location):
                status_code = 200
            else:
                status_code = 404
                location = folder + "/" + not_found_page
            if os.path.exists(location) and not os.path.isdir(location):
                with open(location, "rb") as binary:
                    buffer = binary.read()
                headers["Content-Length"] = len(buffer)
                extension = os.path.splitext(location)[1]
                content_type = mimetypes.types_map[extension]
                if content_type:
                    headers["Content-Type"] = content_type
        print(str(status_code) + " " + self.command + " " + self.path)
        sys.stdout.flush()
        self.send_response(status_code)
        for key in headers:
            self.send_header(key, headers[key])
        self.end_headers()
        if self.command != "HEAD":
            if status_code == 404 and buffer == None:
                self.wfile.write(str(status_code))
            elif (status_code == 200 or status_code == 404) and buffer != None:
                self.wfile.write(buffer)
    def do_GET(self):
        self.handler();
    def do_HEAD(self):
        self.handler();
    def log_message(self, format, *args):
        return

server = HTTPServer(("localhost", port), HTTPRequestHandler)
url = "http://localhost:" + str(port)
print("Serving '" + folder + "' at " + url + "...")
if browse:
    threading.Timer(1, webbrowser.open, args=(url,)).start()
sys.stdout.flush()
try:
    server.serve_forever()
except (KeyboardInterrupt, SystemExit):
    server.server_close()
