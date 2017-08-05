package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var configuration map[string]interface{}
var environment string

var entityMap = strings.NewReplacer(
	`&`, "&amp;", `<`, "&lt;", `>`, "&gt;", `"`, "&quot;", `'`, "&#39;", `/`, "&#x2F;", "`", "&#x60;", `=`, "&#x3D;",
)

func escapeHTML(text string) string {
	return entityMap.Replace(text)
}

func merge(maps ...map[string]interface{}) map[string]interface{} {
	target := make(map[string]interface{})
	for _, obj := range maps {
		for key, value := range obj {
			target[key] = value
		}
	}
	return target
}

var sectionRegex = regexp.MustCompile("{{#\\s*([-_\\/\\.\\w]+)\\s*}}\\s?")
var partialRegex = regexp.MustCompile("{{>\\s*([-_/.\\w]+)\\s*}}")
var replaceRegex = regexp.MustCompile("{{{\\s*([-_/.\\w]+)\\s*}}}")
var escapeRegex = regexp.MustCompile("{{\\s*([-_/.\\w]+)\\s*}}")

func mustache(template string, view map[string]interface{}, partials func(string) string) string {
	for index := 0; index < len(template); {
		if match := sectionRegex.FindStringIndex(template[index:]); match != nil {
			name := sectionRegex.FindStringSubmatch(template[index+match[0] : index+match[1]])[1]
			start := index + match[0]
			index += match[1]
			if match := regexp.MustCompile("{{\\/\\s*" + name + "\\s*}}\\s?").FindStringIndex(template[index:]); match != nil {
				content := template[index : index+match[0]]
				if value, ok := view[name]; ok {
					switch value := value.(type) {
					case []interface{}:
						output := make([]string, len(value))
						for index, item := range value {
							context := merge(view, item.(map[string]interface{}))
							output[index] = mustache(content, context, partials)
						}
						content = strings.Join(output, "")
					case bool:
						if !value {
							content = ""
						}
					}
					template = template[0:start] + content + template[index+match[1]:]
					index = start
				}
			}
		} else {
			index = len(template)
		}
	}
	template = partialRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := partialRegex.FindStringSubmatch(match)[1]
		return mustache(partials(name), view, partials)
	})
	template = replaceRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := replaceRegex.FindStringSubmatch(match)[1]
		if value, ok := view[name]; ok {
			switch value := value.(type) {
			case func() string:
				return mustache(value(), view, partials)
			case string:
				return mustache(value, view, partials)
			}
		}
		return match
	})
	template = escapeRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := escapeRegex.FindStringSubmatch(match)[1]
		if value, ok := view[name]; ok {
			switch value := value.(type) {
			case func() string:
				return escapeHTML(value())
			case string:
				return escapeHTML(value)
			}
		}
		return match
	})
	return template
}

func host(request *http.Request) string {
	if host, ok := configuration["host"]; ok {
		return host.(string)
	}
	scheme := "http"
	if value := request.Header.Get("x-forwarded-proto"); len(value) > 0 {
		scheme = value
	}
	if value := request.Header.Get("x-forwarded-protocol"); len(value) > 0 {
		scheme = value
	}
	return scheme + "://" + request.Host
}

func formatDate(date time.Time, format string) string {
	switch format {
	case "atom":
		return date.UTC().Format("2006-01-02T15:04:05Z")
	case "rss":
		return date.UTC().Format("Mon, 02 Jan 2006 15:04:05 +0000")
	case "user":
		return date.Format("Jan 2, 2006")
	}
	return ""
}

var cacheData = make(map[string]interface{})
var cacheLock = &sync.Mutex{}

func cache(key string, callback func() interface{}) interface{} {
	if environment == "production" {
		cacheLock.Lock()
		value, ok := cacheData[key]
		cacheLock.Unlock()
		if !ok {
			value = callback()
			cacheLock.Lock()
			cacheData[key] = value
			cacheLock.Unlock()
		}
		return value
	}
	return callback()
}

func cacheString(key string, callback func() string) string {
	return cache(key, func() interface{} {
		return callback()
	}).(string)
}

func cacheBuffer(key string, callback func() []byte) []byte {
	return cache(key, func() interface{} {
		return callback()
	}).([]byte)
}

var pathCache = make(map[string]bool)

func initPathCache(dir string) {
	if environment == "production" {
		fileInfos, err := ioutil.ReadDir(dir)
		if err != nil {
			fmt.Println(err)
		} else {
			for _, fileInfo := range fileInfos {
				file := fileInfo.Name()
				if !strings.HasPrefix(file, ".") {
					file = dir + "/" + file
					if fileInfo.IsDir() {
						pathCache[file+"/"] = true
						initPathCache(file)
					} else {
						pathCache[file] = true
					}
				}
				if dir == "." && file == ".well-known" && fileInfo.IsDir() {
					pathCache["./"+file+"/"] = true
					fmt.Println("certificate")
				}
			}
		}
	}
}

func exists(path string) bool {
	if environment == "production" {
		path = "./" + path
		if _, ok := pathCache[path]; ok {
			return true
		}
		if !strings.HasSuffix(path, "/") {
			if _, ok := pathCache[path+"/"]; ok {
				return true
			}
		}
		return false
	}
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func isDir(path string) bool {
	if environment == "production" {
		path = "./" + path
		if !strings.HasSuffix(path, "/") {
			path = path + "/"
		}
		_, ok := pathCache[path]
		return ok
	}
	stat, err := os.Stat(path)
	if err != nil {
		fmt.Println(err)
		return false
	}
	return stat.IsDir()
}

var tagRegexp = regexp.MustCompile("<(\\w+)[^>]*>")
var entityRegexp = regexp.MustCompile("(#?[A-Za-z0-9]+;)")
var truncateMap = map[string]bool{
	"pre": true, "code": true, "img": true, "table": true, "style": true, "script": true, "h2": true, "h3": true,
}

func truncate(text string, length int) string {
	closeTags := make(map[int]string)
	ellipsis := ""
	count := 0
	index := 0
	for count < length && index < len(text) {
		if text[index] == '<' {
			if closeTag, ok := closeTags[index]; ok {
				delete(closeTags, index)
				index += len(closeTag)
			} else {
				match := tagRegexp.FindStringSubmatch(text[index:])
				if len(match) > 0 {
					tag := strings.ToLower(match[1])
					if value, ok := truncateMap[tag]; ok && value {
						break
					}
					index += len(match[0])
					if match := regexp.MustCompile("(?i)</" + tag + "\\s*>").FindStringIndex(text[index:]); match != nil {
						closeTags[index+match[0]] = "</" + tag + ">"
					}
				} else {
					index++
					count++
				}
			}
		} else if text[index] == '&' {
			index++
			if entity := entityRegexp.FindString(text[index:]); len(entity) > 0 {
				index += len(entity)
			}
			count++
		} else {
			if text[index] == ' ' {
				index++
				count++
			}
			skip := strings.IndexAny(text[index:], " <&")
			if skip == -1 {
				skip = len(text) - index
			}
			if count+skip > length {
				ellipsis = "&hellip;"
			}
			if count+skip-15 > length {
				skip = length - count
			}
			index += skip
			count += skip
		}
	}
	output := []string{}
	output = append(output, text[0:index])
	if len(ellipsis) > 0 {
		output = append(output, ellipsis)
	}
	keys := []int{}
	for key := range closeTags {
		keys = append(keys, key)
	}
	sort.Sort(sort.IntSlice(keys))
	for _, key := range keys {
		if closeTag, ok := closeTags[key]; ok {
			output = append(output, closeTag)
		}
	}
	return strings.Join(output, "")
}

func posts() []string {
	return append([]string{}, cache("blog:*", func() interface{} {
		folders := []string{}
		fileInfos, _ := ioutil.ReadDir("blog/")
		for i := len(fileInfos) - 1; i >= 0; i-- {
			if fileInfos[i].IsDir() {
				post := fileInfos[i].Name()
				_, err := os.Stat("blog/" + post + "/index.html")
				if (!os.IsNotExist(err)) {
					folders = append(folders, post)
				}
			}
		}
		return folders
	}).([]string)...)
}

func loadPost(path string) map[string]interface{} {
	if exists(path) && !isDir(path) {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			fmt.Println(err)
		} else {
			item := make(map[string]interface{})
			content := []string{}
			metadata := -1
			lines := regexp.MustCompile("\\r\\n?|\\n").Split(string(data), -1)
			for len(lines) > 0 {
				line := lines[0]
				lines = lines[1:]
				if strings.HasPrefix(line, "---") {
					metadata++
				} else if metadata == 0 {
					index := strings.Index(line, ":")
					if index >= 0 {
						name := strings.Trim(strings.Trim(line[0:index], " "), "\"")
						value := strings.Trim(strings.Trim(line[index+1:], " "), "\"")
						item[name] = value
					}
				} else {
					content = append(content, line)
				}
			}
			item["content"] = strings.Join(content, "\n")
			return item
		}
	}
	return nil
}

func renderPost(file string, host string) string {
	if strings.HasPrefix(file, "blog/") && strings.HasSuffix(file, "/index.html") {
		item := loadPost(file)
		if item != nil {
			if _, ok := item["date"]; ok {
				if date, e := time.Parse("2006-01-02 15:04:05 -07:00", item["date"].(string)); e == nil {
					item["date"] = formatDate(date, "user")
				}
			}
			if _, ok := item["author"]; !ok {
				item["author"] = configuration["name"].(string)
			}
			view := merge(configuration, item)
			view["/"] = "/"
			view["host"] = host
			template, err := ioutil.ReadFile("./blog/post.html")
			if err != nil {
				fmt.Println(err)
			} else {
				return mustache(string(template), view, func(name string) string {
					data, err := ioutil.ReadFile(name)
					if err != nil {
						fmt.Println(err)
						return ""
					}
					return string(data)
				})
			}
		}
	}
	return ""
}

func renderBlog(folders []string, start int) string {
	items := make([]interface{}, 0)
	view := make(map[string]interface{})
	count := 10
	index := 0
	for len(folders) > 0 && index < start+count {
		folder := folders[0]
		folders = folders[1:]
		item := loadPost("blog/" + folder + "/index.html")
		if item != nil && (item["state"] == "post" || environment != "production") {
			if index >= start {
				item["url"] = "/blog/" + folder + "/"
				if _, ok := item["date"]; ok {
					if date, e := time.Parse("2006-01-02 15:04:05 -07:00", item["date"].(string)); e == nil {
						item["date"] = formatDate(date, "user")
					}
				}
				content := item["content"].(string)
				content = regexp.MustCompile("\\s\\s").ReplaceAllString(content, " ")
				truncated := truncate(content, 250)
				item["content"] = truncated
				item["more"] = truncated != content
				items = append(items, item)
			}
			index++
		}
	}
	view["items"] = items
	placeholder := make([]interface{}, 0)
	if len(folders) > 0 {
		placeholder = append(placeholder, map[string]interface{}{"url": "/blog/page" + strconv.Itoa(index) + ".html" })
	}
	view["placeholder"] = placeholder
	template, err := ioutil.ReadFile("./blog/feed.html")
	if err != nil {
		fmt.Println(err)
		return ""
	}
	return mustache(string(template), view, nil)
}

func writeString(response http.ResponseWriter, request *http.Request, contentType string, text string) {
	response.Header().Set("Content-Type", contentType)
	response.Header().Set("Content-Length", strconv.Itoa(bytes.NewBufferString(text).Len()))
	if request.Method != "HEAD" {
		io.WriteString(response, text)
	}
}

func rootHandler(response http.ResponseWriter, request *http.Request) {
	http.Redirect(response, request, "/", http.StatusFound)
}

func feedHandler(response http.ResponseWriter, request *http.Request) {
	pathname := request.URL.Path
	filename := path.Base(pathname)
	format := strings.TrimPrefix(path.Ext(filename), ".")
	host := host(request)
	url := host + pathname
	data := cacheString("feed:"+url, func() string {
		count := 10
		items := make([]interface{}, 0)
		feed := map[string]interface{}{
			"name":        configuration["name"],
			"description": configuration["description"],
			"author":      configuration["name"],
			"host":        host,
			"url":         url,
		}
		recentFound := false
		recent := time.Now()
		folders := posts()
		for len(folders) > 0 && count > 0 {
			folder := folders[0]
			folders = folders[1:]
			item := loadPost("blog/" + folder + "/index.html")
			if item != nil && (item["state"] == "post" || environment != "production") {
				item["url"] = host + "/blog/" + folder + "/"
				if author, ok := item["author"]; !ok || author.(string) == configuration["name"].(string) {
					item["author"] = false
				}
				if _, ok := item["date"]; ok {
					if date, err := time.Parse("2006-01-02 15:04:05 -07:00", item["date"].(string)); err == nil {
						updated := date
						if _, ok := item["updated"]; ok {
							if temp, err := time.Parse("2006-01-02 15:04:05 -07:00", item["updated"].(string)); err == nil {
								updated = temp
							}
						}
						item["date"] = formatDate(date, format)
						item["updated"] = formatDate(updated, format)
						if !recentFound || recent.Before(updated) {
							recent = updated
							recentFound = true;
						}
					}
				}
				item["content"] = escapeHTML(truncate(item["content"].(string), 4000))
				items = append(items, item)
				count--
			}
		}
		feed["updated"] = formatDate(recent, format)
		feed["items"] = items
		template, err := ioutil.ReadFile("./blog/" + filename)
		if err != nil {
			fmt.Println(err)
		} else {
			return mustache(string(template), feed, nil)
		}
		return ""
	})
	writeString(response, request, "application/"+format+"+xml", data)
}

var blogRegexp = regexp.MustCompile("/blog/page(.*).html")

func blogHandler(response http.ResponseWriter, request *http.Request) {
	pathname := strings.ToLower(path.Clean(request.URL.Path))
	if match := blogRegexp.FindStringIndex(pathname); match != nil {
		id := blogRegexp.FindStringSubmatch(pathname[match[0] : match[1]])[1]
		if start, e := strconv.Atoi(id); e == nil {
			folders := posts()
			data := ""
			if start < len(folders) {
				data = cacheString("default:"+pathname, func() string {
					return renderBlog(folders, start)
				})
			}
			writeString(response, request, "text/html", data)
			return
		}
	}
	rootHandler(response, request)
}

func defaultHandler(response http.ResponseWriter, request *http.Request) {
	urlpath := request.URL.Path
	pathname := strings.ToLower(path.Clean(urlpath))
	if pathname != "/" && strings.HasSuffix(urlpath, "/") {
		pathname += "/"
	}
	if strings.HasSuffix(pathname, "/index.html") {
		http.Redirect(response, request, "/"+strings.TrimLeft(pathname[0:len(pathname)-11], "/"), http.StatusMovedPermanently)
		return
	}
	file := pathname
	if strings.HasSuffix(pathname, "/") {
		file = path.Join(pathname, "index.html")
	}
	file = strings.TrimLeft(file, "/")
	if !exists(file) {
		http.Redirect(response, request, path.Dir(pathname), http.StatusFound)
		return
	}
	if isDir(file) {
		http.Redirect(response, request, pathname+"/", http.StatusFound)
		return
	}
	extension := path.Ext(file)
	contentType := mime.TypeByExtension(extension)
	if len(contentType) > 0 && strings.Split(contentType, ";")[0] != "text/html" {
		buffer := cacheBuffer("default:"+file, func() []byte {
			data, err := ioutil.ReadFile(file)
			if err != nil {
				fmt.Println(err)
				return []byte{}
			}
			return data
		})
		if request.Method != "HEAD" {
			response.Write(buffer)
		}
		response.Header().Set("Content-Type", contentType)
		response.Header().Set("Content-Length", strconv.Itoa(len(buffer)))
		response.Header().Set("Cache-Control", "private, max-age=0")
		response.Header().Set("Expires", "-1")
		return
	}
	data := cacheString("default:"+file, func() string {
		post := renderPost(file, host(request))
		if len(post) > 0 {
			return post
		}
		template, err := ioutil.ReadFile(file)
		if err != nil {
			fmt.Println(err)
		} else {
			view := merge(configuration)
			view["/"] = "/"
			view["host"] = host(request)
			view["blog"] = func() string {
				return renderBlog(posts(), 0)
			}
			return mustache(string(template), view, func(name string) string {
				data, err := ioutil.ReadFile(name)
				if err != nil {
					fmt.Println(err)
					return ""
				}
				return string(data)
			})
		}
		return ""
	})
	writeString(response, request, "text/html", data)
}

func certHandler(response http.ResponseWriter, request *http.Request) {
	file := strings.TrimLeft(path.Clean(request.URL.Path), "/")
	if exists(".well-known/") && isDir(".well-known/") {
		if stat, e := os.Stat(file); !os.IsNotExist(e) && !stat.IsDir() {
			data, err := ioutil.ReadFile(file)
			if err != nil {
				fmt.Println(err)
			} else {
				writeString(response, request, "text/plain; charset=utf-8", string(data))
				return
			}
		}
	}
	response.WriteHeader(http.StatusNotFound)
}

type router struct {
	routes []*route
}

type route struct {
	pattern  string
	regexp   *regexp.Regexp
	handlers map[string]interface{}
}

func newRouter(configuration map[string]interface{}) *router {
	router := &router{make([]*route, 0)}
	if redirects, ok := configuration["redirects"]; ok {
		for _, redirect := range redirects.([]interface{}) {
			pattern := redirect.(map[string]interface{})["pattern"].(string)
			target := redirect.(map[string]interface{})["target"].(string)
			router.Get(pattern, target)
		}
	}
	return router
}

func (router *router) Get(pattern string, handler interface{}) {
	router.route(pattern).handlers["GET"] = handler
}

func (router *router) route(pattern string) *route {
	for _, route := range router.routes {
		if pattern == route.pattern {
			return route
		}
	}
	route := &route{
		pattern,
		regexp.MustCompile("^" + strings.Replace(strings.Replace(pattern, ".", "\\.", -1), "*", "(.*)", -1) + "$"),
		make(map[string]interface{}),
	}
	router.routes = append(router.routes, route)
	return route
}

func (router *router) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	fmt.Println(request.Method + " " + request.RequestURI)
	urlpath := request.URL.Path
	pathname := strings.ToLower(path.Clean(urlpath))
	if pathname != "/" && strings.HasSuffix(urlpath, "/") {
		pathname += "/"
	}
	for _, route := range router.routes {
		if route.regexp.MatchString(pathname) {
			method := strings.ToUpper(request.Method)
			if method == "HEAD" {
				if _, ok := route.handlers["HEAD"]; !ok {
					method = "GET"
				}
			}
			if handler, ok := route.handlers[method]; ok {
				if callback, ok := handler.(func(response http.ResponseWriter, request *http.Request)); ok {
					callback(response, request)
					return
				}
				if redirect, ok := handler.(string); ok {
					http.Redirect(response, request, redirect, http.StatusMovedPermanently)
					return
				}
			}
		}
	}
}

func main() {
	fmt.Println(runtime.Version())
	file, err := ioutil.ReadFile("./app.json")
	if err != nil {
		fmt.Println(err)
		return
	}
	err = json.Unmarshal(file, &configuration)
	if err != nil {
		fmt.Println(err)
		return
	}
	environment = os.Getenv("GO_ENV")
	fmt.Println(environment)
	initPathCache(".")
	router := newRouter(configuration)
	router.Get("/blog/feed.atom", feedHandler)
	router.Get("/blog/feed.rss", feedHandler)
	router.Get("/blog/page*.html", blogHandler)
	router.Get("/.well-known/acme-challenge/*", certHandler)
	router.Get("/*", defaultHandler)
	port := 8080
	fmt.Println("http://localhost:" + strconv.Itoa(port))
	http.ListenAndServe(":"+strconv.Itoa(port), router)
}
