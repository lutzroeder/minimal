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

func merge(maps... map[string]interface{}) map[string]interface{} {
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
			name := sectionRegex.FindStringSubmatch(template[index+match[0]:index+match[1]])[1]
			start := index + match[0]
			index += match[1]
			if match := regexp.MustCompile("{{\\/\\s*" + name + "\\s*}}\\s?").FindStringIndex(template[index:]); match != nil {
				content := template[index:index+match[0]]
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
				return value();
			case string:
				return value;
			}
		}
		return match
	})
	template = escapeRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := escapeRegex.FindStringSubmatch(match)[1]
		if value, ok := view[name]; ok {
			switch value := value.(type) {
			case func() string:
				return escapeHTML(value());
			case string:
				return escapeHTML(value);
			}
		}
		return match
	})
	return template
}

func scheme(request *http.Request) string {
	if scheme := request.Header.Get("x-forwarded-proto"); len(scheme) > 0 {
		return scheme
	}
	if scheme := request.Header.Get("x-forwarded-protocol"); len(scheme) > 0 {
		return scheme
	}
	return "http"
}

func formatDate(date time.Time) string {
	return date.UTC().Format("2006-01-02T15:04:05Z")
}

func formatUserDate(text string) string {
	if date, e := time.Parse("2006-01-02 15:04:05 -07:00", text); e == nil {
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
	return append([]string{}, cache("blog:files", func() interface{} {
		files := []string{}
		fileInfos, _ := ioutil.ReadDir("blog/")
		for i := len(fileInfos) - 1; i >= 0; i-- {
			file := fileInfos[i].Name()
			if path.Ext(file) == ".html" {
				files = append(files, file)
			}
		}
		return files
	}).([]string)...)
}

func loadPost(path string) map[string]interface{} {
	if exists(path) && !isDir(path) {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			fmt.Println(err)
		} else {
			entry := make(map[string]interface{})
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
						entry[name] = value
					}
				} else {
					content = append(content, line)
				}
			}
			entry["content"] = strings.Join(content, "\n")
			return entry
		}
	}
	return nil
}

func renderBlog(files []string, start int) string {
	entries := make([]interface{}, 0)
	view := make(map[string]interface{}) 
	length := 10
	index := 0
	for len(files) > 0 && index < start+length {
		file := files[0]
		files = files[1:]
		entry := loadPost("blog/" + file)
		if entry != nil && (entry["state"] == "post" || environment != "production") {
			if index >= start {
				entry["url"] = "/blog/" + strings.TrimSuffix(path.Base(file), ".html")
				if date, ok := entry["date"]; ok {
					entry["date"] = formatUserDate(date.(string))
				}
				content := entry["content"].(string)
				content = regexp.MustCompile("\\s\\s").ReplaceAllString(content, " ")
				truncated := truncate(content, 250)
				entry["content"] = truncated
				entry["more"] = truncated != content
				entries = append(entries, entry)
			}
			index++
		}
	}
	view["entries"] = entries
	placeholder := make([]interface{}, 0)
	if len(files) > 0 {
		placeholder = append(placeholder, map[string]interface{}{"url": "/blog?id=" + strconv.Itoa(index)})
	}
	view["placeholder"] = placeholder
	template, err := ioutil.ReadFile("./stream.html")
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

func atomHandler(response http.ResponseWriter, request *http.Request) {
	host := scheme(request) + "://" + request.Host
	data := cacheString("atom:"+host+"/blog/atom.xml", func() string {
		count := 10
		output := []string{}
		output = append(output,
			"<?xml version='1.0' encoding='UTF-8'?>",
			"<feed xmlns='http://www.w3.org/2005/Atom'>",
			"<title>"+configuration["name"].(string)+"</title>",
			"<id>"+host+"/</id>",
			"<icon>"+host+"/favicon.ico</icon>")
		index := len(output)
		recent := ""
		output = append(output, 
			"",
			"<author><name>"+configuration["name"].(string)+"</name></author>",
			"<link rel='alternate' type='text/html' href='"+host+"/' />",
			"<link rel='self' type='application/atom+xml' href='"+host+"/blog/atom.xml' />")
		files := posts()
		for len(files) > 0 && count > 0 {
			file := files[0]
			files = files[1:]
			entry := loadPost("blog/" + file)
			if entry != nil && (entry["state"] == "post" || environment != "production") {
				url := host + "/blog/" + strings.TrimSuffix(path.Base(file), ".html")
				output = append(output, 
					"<entry>",
					"<id>"+url+"</id>")
				if author, ok := entry["author"]; ok && author != configuration["name"].(string) {
					output = append(output, "<author><name>"+author.(string)+"</name></author>")
				}
				date := ""
				if value, ok := entry["date"]; ok {
					if time, err := time.Parse("2006-01-02 15:04:05 -07:00", value.(string)); err == nil {
						date = formatDate(time)
					}
				}
				updated := date
				if value, ok := entry["updated"]; ok {
					if time, err := time.Parse("2006-01-02 15:04:05 -07:00", value.(string)); err == nil {
						updated = formatDate(time)
					}
				}
				if len(recent) == 0 || recent < updated {
					recent = updated
				}
				content := entry["content"].(string)
				content = escapeHTML(truncate(content, 4000))
				output = append(output,
					"<published>"+date+"</published>",
					"<updated>"+updated+"</updated>",
					"<title type='text'>"+entry["title"].(string)+"</title>",
					"<content type='html'>"+content+"</content>",
					"<link rel='alternate' type='text/html' href='"+url+"' title='"+entry["title"].(string)+"' />",
					"</entry>")
				count--
			}
		}
		if len(recent) == 0 {
			recent = formatDate(time.Now())
		}
		output[index] = "<updated>" + recent + "</updated>"
		output = append(output, "</feed>")
		return strings.Join(output, "\n")
	})
	writeString(response, request, "application/atom+xml", data)
}

func postHandler(response http.ResponseWriter, request *http.Request) {
	file := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	data := cacheString("post:"+file, func() string {
		entry := loadPost(file + ".html")
		if entry != nil {
			if date, ok := entry["date"]; ok {
				entry["date"] = formatUserDate(date.(string))
			}
			if _, ok := entry["author"]; !ok {
				entry["author"] = configuration["name"].(string)
			}
			view := merge(configuration, entry)
			template, err := ioutil.ReadFile("./post.html")
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
		return ""
	})
	if len(data) > 0 {
		writeString(response, request, "text/html", data)
		return
	}
	extension := path.Ext(file)
	contentType := mime.TypeByExtension(extension)
	if len(contentType) > 0 {
		defaultHandler(response, request)
		return
	}
	rootHandler(response, request)
}

func blogHandler(response http.ResponseWriter, request *http.Request) {
	id := request.URL.Query().Get("id")
	if start, e := strconv.Atoi(id); e == nil {
		files := posts()
		data := ""
		if start < len(files) {
			data = cacheString("blog:/blog?id="+id, func() string {
				return renderBlog(files, start)
			})
		}
		writeString(response, request, "text/html", data)
		return
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
		template, err := ioutil.ReadFile(file)
		if err != nil {
			fmt.Println(err)
		} else {
			view := merge(configuration)
			view["feed"] = func() string {
				if feed, ok := configuration["feed"]; ok && len(feed.(string)) > 0 {
					return feed.(string)
				}
				return scheme(request) + "://" + request.Host + "/blog/atom.xml"
			}
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
	pattern string
	regexp *regexp.Regexp
	handlers map[string]interface{}
}

func newRouter(configuration map[string]interface{}) *router {
	router := &router{make([]*route, 0)}
	if redirects, ok := configuration["redirects"]; ok {
		for _, redirect := range redirects.([]interface{}) {
			pattern := redirect.(map[string]interface{})["pattern"].(string)
			target := redirect.(map[string]interface{})["target"].(string)
			router.Get(pattern, target);
		}
	}
	return router
}

func (router *router) Get(pattern string, handler interface{}) {
	router.route(pattern).handlers["GET"] = handler;
}

func (router *router) route(pattern string) *route {
	for _, route := range router.routes {
		if pattern == route.pattern {
			return route
		}
	}
	route := &route{
		pattern,
		regexp.MustCompile("^" + strings.Replace(pattern, "*", "(.*)", -1) + "$"),
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
	router.Get("/.git/?*", rootHandler)
	router.Get("/.vscode/?*", rootHandler)
	router.Get("/admin*", rootHandler)
	router.Get("/app.*", rootHandler)
	router.Get("/header.html", rootHandler)
	router.Get("/meta.html", rootHandler)
	router.Get("/package.json", rootHandler)
	router.Get("/post.html", rootHandler)
	router.Get("/post.css", rootHandler)
	router.Get("/site.css", rootHandler)
	router.Get("/blog/atom.xml", atomHandler)
	router.Get("/blog/*", postHandler)
	router.Get("/blog", blogHandler)
	router.Get("/.well-known/acme-challenge/*", certHandler)
	router.Get("/*", defaultHandler)
	port := 8080
	fmt.Println("http://localhost:" + strconv.Itoa(port))
	http.ListenAndServe(":" + strconv.Itoa(port), router)
}
