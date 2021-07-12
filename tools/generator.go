package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var configuration map[string]interface{}
var environment string
var destination = "build"
var theme = "default"

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
	folders := []string{}
	items, _ := ioutil.ReadDir("content/blog/")
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.IsDir() {
			post := item.Name()
			if _, err := os.Stat("content/blog/" + post + "/index.html"); !os.IsNotExist(err) {
				folders = append(folders, post)
			}
		}
	}
	return folders
}

func loadPost(path string) map[string]interface{} {
	if stat, err := os.Stat(path); !os.IsNotExist(err) && !stat.IsDir() {
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

func renderBlog(folders []string, destination string, root string, page int) string {
	items := make([]interface{}, 0)
	view := make(map[string]interface{})
	count := 10
	for count > 0 && len(folders) > 0 {
		folder := folders[0]
		folders = folders[1:]
		item := loadPost("content/blog/" + folder + "/index.html")
		if item != nil && (item["state"] == "post" || environment != "production") {
			item["url"] = "blog/" + folder + "/"
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
			count--
		}
	}
	view["items"] = items
	placeholder := make([]interface{}, 0)
	if len(folders) > 0 {
		page++
		location := "blog/page" + strconv.Itoa(page) + ".html"
		placeholder = append(placeholder, map[string]interface{}{"url": root + location})
		file := destination + "/" + location
		data := renderBlog(folders, destination, root, page)
		ioutil.WriteFile(file, []byte(data), os.ModePerm)
	}
	view["placeholder"] = placeholder
	view["root"] = root
	template, err := ioutil.ReadFile(path.Join("themes/" + theme + "/feed.html"))
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

func renderPost(source string, destination string, root string) bool {
	if strings.HasPrefix(source, "content/blog/") && strings.HasSuffix(source, "/index.html") {
		item := loadPost(source)
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
			view["root"] = root
			template, err := ioutil.ReadFile("themes/" + theme + "/post.html")
			if err != nil {
				fmt.Println(err)
			} else {
				data := mustache(string(template), view, func(name string) string {
					data, err := ioutil.ReadFile("themes/" + theme + "/" + name)
					if err != nil {
						fmt.Println(err)
						return ""
					}
					return string(data)
				})
				ioutil.WriteFile(destination, []byte(data), os.ModePerm)
				return true
			}
		}
	}
	return false
}

func renderFile(source string, destination string) {
	data, err := ioutil.ReadFile(source)
	if err == nil {
		err = ioutil.WriteFile(destination, data, os.ModePerm)
	}
	if err != nil {
		fmt.Println(err)
	}
}

func renderFeed(source string, destination string) {
	host := configuration["host"].(string)
	format := strings.TrimPrefix(path.Ext(source), ".")
	url := host + "/blog/feed." + format
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
		item := loadPost("content/blog/" + folder + "/index.html")
		if item != nil && item["state"] == "post" {
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
						recentFound = true
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
	template, err := ioutil.ReadFile("content/blog/feed." + format)
	if err != nil {
		fmt.Println(err)
	} else {
		data := mustache(string(template), feed, nil)
		ioutil.WriteFile(destination, []byte(data), os.ModePerm)
	}
}

func renderPage(source string, destination string, root string) {
	if renderPost(source, destination, root) {
		return
	}
	template, err := ioutil.ReadFile(source)
	if err != nil {
		fmt.Println(err)
	} else {
		view := merge(configuration)
		view["root"] = root
		view["blog"] = func() string {
			return renderBlog(posts(), path.Dir(destination), root+"../", 0) +
				`<script type="text/javascript">
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
		}
		pages := make([]interface{}, 0)
		for _, item := range configuration["pages"].([]interface{}) {
			page := item.(map[string]interface{})
			location := path.Dir(source)
			target := mustache(page["url"].(string), view, nil)
			active := path.Join(location, target) == location
			if visible, ok := page["visible"].(bool); (ok && visible) || active {
				pages = append(pages, map[string]interface{}{"name": page["name"].(string), "url": page["url"].(string), "active": active})
			}
		}
		view["pages"] = pages
		data := mustache(string(template), view, func(name string) string {
			data, err := ioutil.ReadFile("themes/" + theme + "/" + name)
			if err != nil {
				fmt.Println(err)
			}
			return string(data)
		})
		ioutil.WriteFile(destination, []byte(data), os.ModePerm)
	}
}

func render(source string, destination string, root string) {
	fmt.Println(destination)
	extension := path.Ext(source)
	switch extension {
	case ".rss", ".atom":
		renderFeed(source, destination)
	case ".html":
		renderPage(source, destination, root)
	default:
		renderFile(source, destination)
	}
}

func renderDir(source string, destination string, root string) {
	os.MkdirAll(destination, os.ModePerm)
	location := source
	if items, err := ioutil.ReadDir(location); err == nil {
		for _, item := range items {
			name := item.Name()
			if !strings.HasPrefix(name, ".") {
				if item.IsDir() {
					renderDir(source+name+"/", destination+"/"+name, root+"../")
				} else {
					render(source+name, destination+"/"+name, root)
				}
			}
		}
	}
}

func cleanDir(directory string) {
	if items, err := ioutil.ReadDir(directory); err == nil {
		for _, item := range items {
			os.RemoveAll(directory + "/" + item.Name())
		}
	}
}

func main() {
	environment = os.Getenv("ENVIRONMENT")
	fmt.Println("go " + strings.TrimPrefix(runtime.Version(), "go") + " " + environment)
	file, err := ioutil.ReadFile("content.json")
	if err != nil {
		fmt.Println(err)
		return
	}
	err = json.Unmarshal(file, &configuration)
	if err != nil {
		fmt.Println(err)
		return
	}
	args := os.Args[1:]
	for len(args) > 0 {
		arg := args[0]
		args = args[1:]
		if arg == "--theme" && len(args) > 0 {
			theme = args[0]
			args = args[1:]
		} else {
			destination = arg
		}
	}
	cleanDir(destination)
	renderDir("content/", destination, "")
}
