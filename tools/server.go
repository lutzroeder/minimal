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
	"runtime"
	"strconv"
	"strings"
)

var root = "."
var port = ":8080"
var browse = false

type httpHandler struct {
}

func (handler *httpHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	pathname := request.URL.Path
	location := root + pathname
	statusCode := 404
	headers := map[string]string { }
	if stat, err := os.Stat(location); !os.IsNotExist(err) && stat.IsDir() {
		if !strings.HasSuffix(location, "/") {
			statusCode = 302
			headers = map[string]string { 
				"Location": pathname + "/",
			}
		} else {
			location += "index.html"
		}
	}
	buffer := make([]byte, 0)
	if stat, err := os.Stat(location); !os.IsNotExist(err) && !stat.IsDir() {
		extension := path.Ext(location)
		contentType := mime.TypeByExtension(extension)
		if len(contentType) > 0 {
			if data, err := ioutil.ReadFile(location); err == nil {
				buffer = data
				statusCode = 200
				headers = map[string]string { 
					"Content-Type": contentType,
					"Content-Length": strconv.Itoa(len(buffer)),
				}
			}
		}
	}
	fmt.Println(strconv.Itoa(statusCode) + " " + request.Method + " " + request.RequestURI)
	for key, value := range headers {
		response.Header().Set(key, value)
	}
	response.WriteHeader(statusCode)
	if statusCode != 200 {
		response.Write([]byte(strconv.Itoa(statusCode)))
	} else if request.Method != "HEAD" {
		response.Write(buffer)
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
		} else if (arg == "--browse" || arg == "-b") {
			browse = true
		} else {
			root = arg
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
	fmt.Println("Serving '" + root + "' at " + url + "...")
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
