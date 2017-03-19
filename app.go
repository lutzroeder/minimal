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

var partialRegex = regexp.MustCompile("{{>\\s*([-_/.\\w]+)\\s*}}")
var replaceRegex = regexp.MustCompile("{{{\\s*([-_/.\\w]+)\\s*}}}")
var escapeRegex = regexp.MustCompile("{{\\s*([-_/.\\w]+)\\s*}}")

func mustache(template string, context map[string]interface{}, partials interface{}) string {
	template = partialRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := partialRegex.FindStringSubmatch(match)[1]
		value := match
		if f, ok := partials.(func(string) string); ok {
			value = f(name)
		}
		return value
	})
	template = replaceRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := replaceRegex.FindStringSubmatch(match)[1]
		value := match
		if o, ok := context[name]; ok {
			if f, ok := o.(func() string); ok {
				value = f()
			}
			if v, ok := o.(string); ok {
				value = v
			}
		}
		return value
	})
	template = escapeRegex.ReplaceAllStringFunc(template, func(match string) string {
		name := escapeRegex.FindStringSubmatch(match)[1]
		value := match
		if o, ok := context[name]; ok {
			if f, ok := o.(func() string); ok {
				value = f()
			}
			if v, ok := o.(string); ok {
				value = v
			}
		}
		return escapeHTML(value)
	})
	return template
}

func mustReadFile(path string) []byte {
	file, e := ioutil.ReadFile(path)
	if e != nil {
		panic(e)
	}
	return file
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
		fileInfos, e := ioutil.ReadDir(dir)
		if e != nil {
			panic(e)
		}
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
	if stat, err := os.Stat(path); !os.IsNotExist(err) {
		return stat.IsDir()
	}
	return false
}

var tagRegexp = regexp.MustCompile("<(\\w+)[^>]*>")
var entityRegexp = regexp.MustCompile("(#?[A-Za-z0-9]+;)")
var truncateMap = map[string]bool {
	"pre": true, "code": true, "img": true, "table": true, "style": true, "script": true,
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

func loadPost(path string) map[string]string {
	if stat, e := os.Stat(path); !os.IsNotExist(e) && !stat.IsDir() {
		data := string(mustReadFile(path))
		entry := make(map[string]string)
		content := []string{}
		metadata := -1
		lines := regexp.MustCompile("\\r\\n?|\\n").Split(data, -1)
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
	return nil
}

func renderBlog(files []string, start int) string {
	output := []string{}
	length := 10
	index := 0
	for len(files) > 0 && index < start+length {
		file := files[0]
		files = files[1:]
		entry := loadPost("./blog/" + file)
		if entry != nil && (entry["state"] == "post" || environment != "production") {
			if index >= start {
				location := "/blog/" + strings.TrimSuffix(path.Base(file), ".html")
				if date, ok := entry["date"]; ok {
					entry["date"] = formatUserDate(date)
				}
				post := []string{}
				post = append(post, "<div class='item'>")
				post = append(post, "<div class='date'>"+entry["date"]+"</div>")
				post = append(post, "<h1><a href='"+location+"'>"+entry["title"]+"</a></h1>")
				post = append(post, "<div class='content'>")
				content := entry["content"]
				content = regexp.MustCompile("\\s\\s").ReplaceAllString(content, " ")
				truncated := truncate(content, 250)
				post = append(post, truncated)
				post = append(post, "</div>")
				if truncated != content {
					post = append(post, "<div class='more'><a href='"+location+"'>"+"Read more&hellip;"+"</a></div>")
				}
				post = append(post, "</div>")
				output = append(output, strings.Join(post, "\n")+"\n")
			}
			index++
		}
	}
	if len(files) > 0 {
		template := string(mustReadFile("./stream.html"))
		context := map[string]interface{} { "url": "/blog?id=" + strconv.Itoa(index) }
		data := mustache(template, context, nil)
		output = append(output, data)
	}
	return strings.Join(output, "\n")
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
		output = append(output, "<?xml version='1.0' encoding='UTF-8'?>")
		output = append(output, "<feed xmlns='http://www.w3.org/2005/Atom'>")
		output = append(output, "<title>"+configuration["name"].(string)+"</title>")
		output = append(output, "<id>"+host+"/</id>")
		output = append(output, "<icon>"+host+"/favicon.ico</icon>")
		index := len(output)
		recent := ""
		output = append(output, "")
		output = append(output, "<author><name>"+configuration["name"].(string)+"</name></author>")
		output = append(output, "<link rel='alternate' type='text/html' href='"+host+"/' />")
		output = append(output, "<link rel='self' type='application/atom+xml' href='"+host+"/blog/atom.xml' />")
		files := posts()
		for len(files) > 0 && count > 0 {
			file := files[0]
			files = files[1:]
			entry := loadPost("blog/" + file)
			if entry != nil && (entry["state"] == "post" || environment != "production") {
				url := host + "/blog/" + strings.TrimSuffix(path.Base(file), ".html")
				output = append(output, "<entry>")
				output = append(output, "<id>"+url+"</id>")
				if author, ok := entry["author"]; ok && author != configuration["name"].(string) {
					output = append(output, "<author><name>"+author+"</name></author>")
				}
				date := ""
				if value, ok := entry["date"]; ok {
					if time, err := time.Parse("2006-01-02 15:04:05 -07:00", value); err == nil {
						date = formatDate(time)
					}
				}
				output = append(output, "<published>"+date+"</published>")
				updated := date
				if value, ok := entry["updated"]; ok {
					if time, err := time.Parse("2006-01-02 15:04:05 -07:00", value); err == nil {
						updated = formatDate(time)
					}
				}
				output = append(output, "<updated>"+updated+"</updated>")
				if len(recent) == 0 || recent < updated {
					recent = updated
				}
				output = append(output, "<title type='text'>"+entry["title"]+"</title>")
				content := escapeHTML(truncate(entry["content"], 4000))
				output = append(output, "<content type='html'>"+content+"</content>")
				output = append(output, "<link rel='alternate' type='text/html' href='"+url+"' title='"+entry["title"]+"' />")
				output = append(output, "</entry>")
				count--;
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
	file := strings.TrimPrefix(strings.ToLower(path.Clean(request.URL.Path)), "/")
	data := cacheString("post:"+file, func() string {
		entry := loadPost(file + ".html")
		if entry != nil {
			if date, ok := entry["date"]; ok {
				entry["date"] = formatUserDate(date)
			}
			if _, ok := entry["author"]; !ok {
				entry["author"] = configuration["name"].(string)
			}
			context := make(map[string]interface{})
			for key, value := range configuration {
				context[key] = value
			}
			for key, value := range entry {
				context[key] = value
			}
			template := string(mustReadFile("./post.html"))
			return mustache(template, context, func(name string) string {
				return string(mustReadFile(path.Join("./", name)))
			})
		}
		return ""
	})
	if len(data) > 0 {
		writeString(response, request, "text/html", data)
	} else {
		extension := path.Ext(file)
		contentType := mime.TypeByExtension(extension)
		if len(contentType) > 0 {
			defaultHandler(response, request)
		} else {
			rootHandler(response, request)
		}
	}
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
	} else {
		rootHandler(response, request)
	}
}

func defaultHandler(response http.ResponseWriter, request *http.Request) {
	url := request.URL.Path
	pathname := strings.ToLower(path.Clean(request.URL.Path))
	if pathname != "/" && strings.HasSuffix(url, "/") {
		pathname += "/"
	}
	if strings.HasSuffix(pathname, "/index.html") {
		http.Redirect(response, request, "/"+strings.TrimLeft(pathname[0:len(pathname)-11], "/"), http.StatusMovedPermanently)
	} else {
		file := pathname
		if strings.HasSuffix(pathname, "/") {
			file = path.Join(pathname, "index.html")
		}
		file = strings.TrimLeft(file, "/")
		if !exists(file) {
			http.Redirect(response, request, path.Dir(pathname), http.StatusFound)
		} else if isDir(file) {
			http.Redirect(response, request, pathname+"/", http.StatusFound)
		} else {
			extension := path.Ext(file)
			contentType := mime.TypeByExtension(extension)
			if len(contentType) > 0 && strings.Split(contentType, ";")[0] != "text/html" {
				data := cacheBuffer("default:"+file, func() []byte {
					return mustReadFile("./" + file)
				})
				if request.Method != "HEAD" {
					response.Write(data)
				}
				response.Header().Set("Content-Type", contentType)
				response.Header().Set("Content-Length", strconv.Itoa(len(data)))
				response.Header().Set("Cache-Control", "private, max-age=0")
				response.Header().Set("Expires", "-1")
			} else {
				data := cacheString("default:"+file, func() string {
					template := mustReadFile(path.Join("./", file))
					context := make(map[string]interface{})
					for key, value := range configuration {
						context[key] = value
					}
					context["feed"] = func() string {
						if feed, ok := configuration["feed"]; ok && len(feed.(string)) > 0 {
							return feed.(string);
						}
						return scheme(request) + "://" + request.Host + "/blog/atom.xml"
					}
					context["links"] = func() string {
						list := []string{}
						for _, link := range configuration["links"].([]interface{}) {
							name := link.(map[string]interface{})["name"].(string)
							symbol := link.(map[string]interface{})["symbol"].(string)
							url := link.(map[string]interface{})["url"].(string)
							list = append(list, "<a class='icon' target='_blank' href='"+url+"' title='"+name+"'><span class='symbol'>"+symbol+"</span></a>")
						}
						return strings.Join(list, "\n")
					}
					context["tabs"] = func() string {
						list := []string{}
						for _, page := range configuration["pages"].([]interface{}) {
							name := page.(map[string]interface{})["name"].(string)
							url := page.(map[string]interface{})["url"].(string)
							list = append(list, "<li class='tab'><a href='"+url+"'>"+name+"</a></li>")
						}
						return strings.Join(list, "\n")
					}
					context["blog"] = func() string {
						return renderBlog(posts(), 0)
					}
					return mustache(string(template), context, func(name string) string {
						return string(mustReadFile(path.Join("./", name)))
					})
				})
				writeString(response, request, "text/html", data)
			}
		}
	}
}

func certHandler(response http.ResponseWriter, request *http.Request) {
	file := strings.TrimLeft(path.Clean(request.URL.Path), "/")
	found := false
	if exists(".well-known/") && isDir(".well-known/") {
		if stat, e := os.Stat(file); !os.IsNotExist(e) && !stat.IsDir() {
			data := mustReadFile(file)
			response.Header().Set("Content-Type", "text/plain; charset=utf-8")
			response.Header().Set("Content-Length", strconv.Itoa(len(data)))
			response.Write(data)
			found = true
		}
	}
	if !found {
		response.WriteHeader(http.StatusNotFound)
	}
}

type loggerHandler struct {
	handler http.Handler
}

func (logger loggerHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	fmt.Println(request.Method + " " + request.RequestURI)
	logger.handler.ServeHTTP(response, request)
}

func main() {
	fmt.Println(runtime.Version())
	file := mustReadFile("./app.json")
	if e := json.Unmarshal(file, &configuration); e != nil {
		panic(e)
	}
	environment = os.Getenv("GO_ENV")
	fmt.Println(environment)
	initPathCache(".")
	http.HandleFunc("/.git", rootHandler)
	http.HandleFunc("/admin", rootHandler)
	http.HandleFunc("/admin.cfg", rootHandler)
	http.HandleFunc("/app.go", rootHandler)
	http.HandleFunc("/app.js", rootHandler)
	http.HandleFunc("/app.json", rootHandler)
	http.HandleFunc("/app.py", rootHandler)
	http.HandleFunc("/header.html", rootHandler)
	http.HandleFunc("/meta.html", rootHandler)
	http.HandleFunc("/package.json", rootHandler)
	http.HandleFunc("/post.css", rootHandler)
	http.HandleFunc("/post.html", rootHandler)
	http.HandleFunc("/site.css", rootHandler)
	http.HandleFunc("/stream.html", rootHandler)
	http.HandleFunc("/blog/atom.xml", atomHandler)
	http.HandleFunc("/blog/", postHandler)
	http.HandleFunc("/blog", blogHandler)
	http.HandleFunc("/.well-known/acme-challenge/", certHandler)
	http.HandleFunc("/", defaultHandler)
	port := 8080
	fmt.Println("http://localhost:" + strconv.Itoa(port))
	http.ListenAndServe(":8080", loggerHandler{http.DefaultServeMux})
}
