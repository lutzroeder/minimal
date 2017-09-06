package main

import (
	"fmt"
	"io/ioutil"
	"mime"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

type redirect struct {
	Source string
	Target string
}

var folder = "."
var port = ":8080"
var browse = false
var redirects = make([]redirect, 0)
var indexPage = "index.html"
var notFoundPage = ""

type httpHandler struct {
}

func (handler *httpHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	pathname := request.URL.Path
	location := folder + pathname
	statusCode := 0
	headers := map[string]string { }
	buffer := make([]byte, 0)
	for _, redirect := range redirects {
		if (redirect.Source == pathname) {
			statusCode = 301
			headers = map[string]string { "Location": redirect.Target }
			break;
		}
	}
	if statusCode == 0 {
		if stat, err := os.Stat(location); !os.IsNotExist(err) && stat.IsDir() {
			if strings.HasSuffix(location, "/") {
				location += "index.html"
			} else {
				statusCode = 302
				headers = map[string]string { "Location": pathname + "/" }
			}
		}
	}
	if statusCode == 0 {
		if stat, err := os.Stat(location); !os.IsNotExist(err) && !stat.IsDir() {
			statusCode = 200
		} else {
			statusCode = 404
			location = folder + "/" + notFoundPage
		}
		if stat, err := os.Stat(location); !os.IsNotExist(err) && !stat.IsDir() {
			extension := path.Ext(location)
			contentType := mime.TypeByExtension(extension)
			if len(contentType) > 0 {
				headers["Content-Type"] = contentType;
			}
			if data, err := ioutil.ReadFile(location); err == nil {
				buffer = data
				headers["Content-Length"] = strconv.Itoa(len(buffer));
			}
		}
	}
	fmt.Println(strconv.Itoa(statusCode) + " " + request.Method + " " + request.RequestURI)
	for key, value := range headers {
		response.Header().Set(key, value)
	}
	response.WriteHeader(statusCode)
	if request.Method != "HEAD" {
		if _, ok := headers["Content-Length"]; !ok && statusCode == 404 {
			response.Write([]byte(strconv.Itoa(statusCode)))
		} else if _, ok := headers["Content-Length"]; ok && (statusCode == 200 || statusCode == 404) {
			response.Write(buffer)
		}
	}
}

func main() {
	args := os.Args[1:]
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		if (arg == "--port" || arg == "-p") && len(args) > 0 {
			if _, err := strconv.Atoi(args[0]); err == nil {
				port = ":" + args[0]
			}
			args = args[1:]
		} else if (arg == "--index-page" || arg == "-i") && len(args) > 0 {
			indexPage = args[0]
			args = args[1:]
		} else if (arg == "--not-found-page" || arg == "-i") && len(args) > 0 {
			notFoundPage = args[0]
			args = args[1:]
		} else if (arg == "--browse" || arg == "-b") {
			browse = true
		} else if (arg == "--redirect-map" || arg == "-r") && len(args) > 0 {
			path := args[0]
			args = args[1:]
			data, err := ioutil.ReadFile(path)
			if err != nil {
				fmt.Println(err)
				return
			}
			lines := regexp.MustCompile("\\r\\n?|\\n").Split(string(data), -1)
			for len(lines) > 0 {
				line := lines[0]
				lines = lines[1:]
				match := regexp.MustCompile("([^ ]*) *([^ ]*)").FindAllStringSubmatch(line, -1)
				if len(match) > 0 && len(match[0]) > 2 && len(match[0][1]) > 0 && len(match[0][2]) > 0 {
					redirects = append(redirects, redirect{ match[0][1], match[0][2] })
				}
			}
		} else if (arg == "--browse" || arg == "-b") {
			browse = true
		} else if !strings.HasPrefix(arg, "-") {
			folder = arg
		}
	}
	server := http.Server{}
	server.Handler = &httpHandler{}
	listener, err := net.Listen("tcp", port)
	if err != nil {
		fmt.Println(err)
		return
	}
	go server.Serve(listener)
	url := "http://localhost" + port
	fmt.Println("Serving '" + folder + "' at " + url + "...")
	if browse {
		command := "xdg-open"
		arg := url
		switch runtime.GOOS {
			case "darwin": command = "open"
			case "windows": command = "cmd"; arg = "/C start " + arg 
		}
		exec.Command(command, arg).Run()
	}
	exit := make(chan struct{})
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	go func() {
		select {
		case <-quit:
			server.Shutdown(nil)
			close(exit)
		}
	}()
	<-exit
}
