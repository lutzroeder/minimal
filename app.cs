using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Net;
using System.Text;
using System.Text.RegularExpressions;
using System.Threading;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Http;
using Newtonsoft.Json.Linq;

class Program
{
    static IHostingEnvironment environment;
    static IDictionary<string, object> configuration;

    static Dictionary<string,string> entityMap = new Dictionary<string,string>() {
        ["&"] = "&amp;", ["<"] = "&lt;", [">"] = "&gt;", ["\""] = "&quot;",
        ["'"] = "&#39;", ["/"] = "&#x2F;", ["`"] = "&#x60;", ["="] = "&#x3D;"
    };
    static Regex entityRegex = new Regex("[&<>\"\'`=\\/]");

    static string EscapeHtml(string text)
    {
        return entityRegex.Replace(text, (match) => entityMap[match.Groups[0].Value]);
    }

    static IDictionary<string, object> Merge(params IDictionary<string, object>[] maps)
    {
        var target = new Dictionary<string, object>();
        foreach (IDictionary<string, object> map in maps)
        {
            foreach (KeyValuePair<string, object> pair in map)
            {
                target[pair.Key] = pair.Value;
            }
        }
        return target;
    }

    static Regex sectionRegex = new Regex(@"{{#\s*([-_\/\.\w]+)\s*}}\s?([\s\S]*){{\/\1}}\s?");
    static Regex partialRegex = new Regex(@"\{\{>\s*([-_/.\w]+)\s*\}\}");
    static Regex replaceRegex = new Regex(@"{{{\s*([-_/.\w]+)\s*}}}");
    static Regex escapeRegex = new Regex(@"{{\s*([-_/.\w]+)\s*}}");

    static string Mustache(string template, IDictionary<string, object> view, Func<string, string> partials)
    {
        template = sectionRegex.Replace(template, delegate(Match match) {
            string name = match.Groups[1].Value;
            string content = match.Groups[2].Value;
            object section;
            if (view.TryGetValue(name, out section))
            {
                switch (section)
                {
                    case IEnumerable<object> list:
                        return string.Join("", list.Select(item => Mustache(content, Merge(view, (Dictionary<string, object>) item), partials)));
                    case bool value:
                        return value ? Mustache(content, view, partials) : "";
                }
            }
            return "";
        });
        template = partialRegex.Replace(template, delegate(Match match) {
            string name = match.Groups[1].Value;
            return Mustache(partials(name), view, partials);
        });
        template = replaceRegex.Replace(template, delegate(Match match) {
            string name = match.Groups[1].Value;
            object value;
            if (view.TryGetValue(name, out value))
            {
                switch (value)
                {
                    case Func<string> callback:
                        return callback();
                    case string content:
                        return content;
                }
            }
            return match.Groups[0].Value;
        });
        template = escapeRegex.Replace(template, delegate(Match match) {
            string name = match.Groups[1].Value;
            object value;
            if (view.TryGetValue(name, out value))
            {
                switch (value)
                {
                    case Func<string> callback:
                        return EscapeHtml(callback());
                    case string content:
                        return EscapeHtml(content);
                }
            }
            return match.Groups[0].Value;
        });

        return template;
    }

    static string FormatDate(DateTime date, string format)
    {
        switch (format)
        {
            case "atom": 
                return date.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ");
            case "rss": 
                return date.ToUniversalTime().ToString("ddd, dd MMM yyyy HH:mm:ss") + " +0000";
            case "user": 
                return date.ToString("MMM d, yyyy");
        }
        return string.Empty;
    }

    static Dictionary<string, object> cacheData = new Dictionary<string, object>();
    static object cacheLock = new object(); 

    static object Cache(string key, Func<object> callback)
    {
        if (environment.IsProduction())
        {
            object value;
            bool exists;
            lock (cacheLock)
            {
                exists = cacheData.TryGetValue(key, out value);
            }
            if (!exists)
            {
                value = callback();
                lock (cacheLock)
                {
                    cacheData[key] = value;
                }
            }
            return value;
        }
        return callback();
    }

    static string CacheString(string key, Func<string> callback)
    {
        return (string) Cache(key, () => (string) callback());
    }

    static byte[] CacheBuffer(string key, Func<byte[]> callback)
    {
        return (byte[]) Cache(key, () => (byte[]) callback());
    }

    static HashSet<string> pathCache = new HashSet<string>();

    static bool Exists(string path)
    {
        if (environment.IsProduction())
        {
            path = "./" + path;
            return pathCache.Contains(path) || (!path.EndsWith("/") && pathCache.Contains(path + "/"));
        }
        return File.Exists(path) || Directory.Exists(path);
    }

    static bool IsDirectory(string path)
    {
        if (environment.IsProduction())
        {
            path = "./" + (path.EndsWith("/") ? path : path + "/");
            return pathCache.Contains(path);
        }
        return Directory.Exists(path);
    }

    static void InitPathCache(string root)
    {
        if (environment.IsProduction())
        {
            foreach (string file in Directory.GetFiles(root))
            {
                string filename = Path.GetFileName(file);
                if (!filename.StartsWith("."))
                {
                    pathCache.Add(root + "/" + filename);
                }
            }
            foreach (string directory in Directory.GetDirectories(root))
            {
                string dirname = Path.GetFileName(directory);
                if (!dirname.StartsWith("."))
                {
                    pathCache.Add(root + "/" + dirname + "/");
                    InitPathCache(root + "/" + dirname);
                }
                if (root == "." && dirname == ".well-known")
                {
                    pathCache.Add("./" + dirname + "/");
                    Console.WriteLine("certificate");
                }
            }
        }
    }

    static Regex tagRegexp = new Regex("<(\\w+)[^>]*>");
    static Regex entityRegexp = new Regex("(#?[A-Za-z0-9]+;)");
    static Regex breakRegexp = new Regex(" |<|&");
    static HashSet<string> truncateMap = new HashSet<string>() {
        "pre", "code", "img", "table", "style", "script", "h2", "h3"
    };

    static string Truncate(string text, int length)
    {
        var closeTags = new SortedDictionary<int, string>();
        var ellipsis = string.Empty;
        var count = 0;
        var index = 0;
        while (count < length && index < text.Length)
        {
            if (text[index] == '<')
            {
                if (closeTags.ContainsKey(index))
                {
                    var closeTagLength = closeTags[index].Length;
                    closeTags.Remove(index);
                    index += closeTagLength;
                } 
                else
                {
                    var match = tagRegexp.Match(text.Substring(index));
                    if (match.Success)
                    {
                        var tag = match.Groups[1].Value.ToLower();
                        if (truncateMap.Contains(tag))
                        {
                            break;
                        }
                        index += match.Groups[0].Length;
                        var closeTagRegExp = new Regex("(</" + tag + "\\s*>)", RegexOptions.IgnoreCase);
                        var end = closeTagRegExp.Match(text.Substring(index));
                        if (end.Success)
                        {
                            closeTags[index + end.Index] = "</" + tag + ">";
                        }
                    }
                    else
                    {
                        index++;
                        count++;
                    }
                }
            }
            else if (text[index] == '&')
            {
                index++;
                var entity = entityRegexp.Match(text.Substring(index));
                if (entity.Success)
                {
                    index += entity.Groups[0].Value.Length;
                }
                count++;
            }
            else
            {
                if (text[index] == ' ')
                {
                    index++;
                    count++;
                }
                var match = breakRegexp.Match(text.Substring(index));
                var skip = match.Success ? match.Index : text.Length - index;
                if (count + skip > length)
                {
                    ellipsis = "&hellip;";
                }
                if (count + skip - 15 > length)
                {
                    skip = length - count;
                }
                index += skip;
                count += skip;
            }
        }
        var output = new List<string>() { text.Substring(0, index) };
        if (!string.IsNullOrEmpty(ellipsis))
        {
            output.Add(ellipsis);
        }
        foreach (KeyValuePair<int, string> pair in closeTags)
        {
            output.Add(pair.Value);
        }
        return string.Join(string.Empty, output);
    }

    static Dictionary<string, object> LoadPost(string file)
    {
        if (Exists(file) && !IsDirectory(file))
        {
            var data = File.ReadAllText(file);
            var lines = new Queue<string>(new Regex("\\r\\n?|\\n").Split(data));
            var item = new Dictionary<string, object>();
            var content = new List<string>();
            int metadata = -1;
            while (lines.Count > 0)
            {
                var line = lines.Dequeue();
                if (line.StartsWith("---"))
                {
                    metadata++;
                }
                else if (metadata == 0)
                {
                    var index = line.IndexOf(":");
                    if (index >= 0)
                    {
                        var name = line.Substring(0, index).Trim();
                        var value = line.Substring(index + 1).TrimStart(' ', '"').TrimEnd(' ', '"');
                        item[name] = value;
                    }
                }
                else
                {
                    content.Add(line);
                }
            }
            item["content"] = string.Join("\n", content);
            return item;
        }
        return null;
    }

    static Queue<string> Posts()
    {
        var posts = (List<string>) Cache("blog:files", delegate() {
            var list = new List<string>(Directory.GetFiles("blog/", "*.html").Select(file => Path.GetFileName(file)));
            list.Sort();
            list.Reverse();
            return list;
        });
        return new Queue<string>(posts);
    }

    static string RenderBlog(Queue<string> files, int start)
    {
        var items = new List<object>();
        var view = new Dictionary<string, object>() { ["items"] = items };
        int length = 10;
        int index = 0;
        while (files.Count > 0 && index < (start + length))
        {
            string file = files.Dequeue();
            var item = LoadPost("blog/" + file);
            if (item != null && (((string)item["state"]) == "post" || !environment.IsProduction()))
            {
                if (index >= start)
                {
                    item["url"] = "/blog/" + Path.GetFileNameWithoutExtension(file);
                    DateTime date;
                    if (item.ContainsKey("date") && DateTime.TryParse(item["date"] as string, out date))
                    {
                        item["date"] = FormatDate(date, "user");
                    }
                    var post = new List<string>();
                    var content = (string) item["content"];
                    content = new Regex("\\s\\s").Replace(content, " ");
                    var truncated = Truncate(content, 250);
                    item["content"] = truncated;
                    item["more"] = truncated != content;
                    items.Add(item);
                }
                index++;
            }
        }
        var placeholder = new List<object>();
        view["placeholder"] = placeholder;
        if (files.Count > 0)
        {
            placeholder.Add(new Dictionary<string, object>() { ["url"] = "/blog?id=" + index.ToString() });
        }
        var template = File.ReadAllText("stream.html");
        return Mustache(template, view, null);
    }

    static string RenderFeed(string format, string host) 
    {
        var url = host + "/" + format + ".xml";
        return CacheString(format + ":" + url, delegate() {
            var count = 10;
            var items = new List<object>();
            var feed = new Dictionary<string, object>() {
                ["name"] = configuration["name"],
                ["description"] = configuration["description"],
                ["author"] = configuration["name"],
                ["host"] = host,
                ["url"] = url,
                ["items"] = items
            };
            var files = Posts();
            var recentFound = false;
            var recent = DateTime.Now;
            while (files.Count > 0 && count > 0) 
            {
                string file = files.Dequeue();
                var item = LoadPost("blog/" + file);
                if (item != null && (((string)item["state"]) == "post" || !environment.IsProduction()))
                {
                    item["url"] = host + "/blog/" + Path.GetFileNameWithoutExtension(file);
                    if (!item.ContainsKey("author") || item["author"] == configuration["name"]) 
                    {
                        item["author"] = false;
                    }
                    DateTime date;
                    if (item.ContainsKey("date") && DateTime.TryParse(item["date"] as string, out date))
                    {
                        DateTime updated;
                        if (!item.ContainsKey("updated") || !DateTime.TryParse(item["updated"] as string, out updated))
                        {
                            updated = date;
                        }
                        item["date"] = FormatDate(date, format);
                        item["updated"] = FormatDate(updated, format);
                        if (!recentFound || recent.CompareTo(updated) < 0)
                        {
                            recent = updated;
                            recentFound = true;
                        }
                    }
                    item["content"] = EscapeHtml(Truncate((string)item["content"], 10000));
                    items.Add(item);
                    count--;
                }
            }
            feed["updated"] = FormatDate(recent, format);
            var template = File.ReadAllText(format + ".xml");
            return Mustache(template, feed, null);
        });
    }

    static Task WriteStringAsync(HttpContext context, string contentType, string data)
    {
        context.Response.ContentType = contentType;
        context.Response.ContentLength = Encoding.UTF8.GetByteCount(data);
        if (context.Request.Method != "HEAD")
        {
            return context.Response.WriteAsync(data);
        }
        return Task.CompletedTask;
    }

    static Task RootHandler(HttpContext context)
    {
        context.Response.Redirect("/");
        return Task.CompletedTask;
    }

    static Task AtomHandler(HttpContext context)
    {
        var host = context.Request.Scheme + "://" + context.Request.Host;
        var data = RenderFeed("atom", host);
        return WriteStringAsync(context, "application/atom+xml", data);
    }

    static Task RssHandler(HttpContext context)
    {
        var host = context.Request.Scheme + "://" + context.Request.Host;
        var data = RenderFeed("rss", host);
        return WriteStringAsync(context, "application/rss+xml", data);
    }

    static Dictionary<string,string> mimeTypeMap = new Dictionary<string,string>() {
        [".js"] = "text/javascript",
        [".css"] = "text/css",
        [".png"] = "image/png",
        [".gif"] = "image/gif",
        [".jpg"] = "image/jpeg",
        [".ico"] = "image/x-icon",
        [".zip"] = "application/zip",
        [".json"] = "application/json",
    };

    static Task PostHandler(HttpContext context)
    {
        var pathname = context.Request.Path.Value;
        var file = pathname.TrimStart('/');
        string data = CacheString("post:" + file, delegate() {
            var item = LoadPost(file + ".html");
            if (item != null)
            {
                item["date"] = string.Empty;
                DateTime date;
                if (item.ContainsKey("date") && DateTime.TryParse(item["date"] as string, out date))
                {
                    item["date"] = FormatDate(date, "user");
                }
                if (!item.ContainsKey("author"))
                {
                    item["author"] = (string) configuration["name"];
                }
                var view = Merge(configuration, item);
                var template = File.ReadAllText("post.html");
                return Mustache(template, view, (name) => File.ReadAllText(name));
            }
            return string.Empty;
        });
        if (!string.IsNullOrEmpty(data))
        {
            return WriteStringAsync(context, "text/html", data);
        }
        var extension = Path.GetExtension(file);
        string contentType;
        if (mimeTypeMap.TryGetValue(extension, out contentType))
        {
            return DefaultHandler(context);
        }
        return RootHandler(context);
    }

    static Task BlogHandler(HttpContext context)
    {
        if (context.Request.Query.ContainsKey("id"))
        {
            int id;
            if (int.TryParse(context.Request.Query["id"], out id))
            {
                var key = "/blog?id=" + id.ToString();
                var files = Posts();
                var data = string.Empty;
                if (id < files.Count)
                {
                    data = CacheString("blog:" + key, () => RenderBlog(files, id));
                }
                return WriteStringAsync(context, "text/html", data);
            }
        }
        return RootHandler(context);
    }

    static Task CertHandler(HttpContext context)
    {
        var file = context.Request.Path.Value.TrimStart('/');
        if (Exists(".well-known/") && IsDirectory(".well-known/"))
        {
            if (File.Exists(file))
            {
                var data = File.ReadAllText(file);
                return WriteStringAsync(context, "text/plain; charset=utf-8", data);
            }
        }
        context.Response.StatusCode = (int) HttpStatusCode.NotFound;
        return Task.CompletedTask;
    }

    static Task DefaultHandler(HttpContext context)
    {
        string pathname = context.Request.Path.Value.ToLower();
        if (pathname.EndsWith("/index.html"))
        {
            context.Response.Redirect("/" + pathname.Substring(0, pathname.Length - 11).TrimStart('/'), true);
            return Task.CompletedTask;
        }
        string file = pathname;
        if (pathname.EndsWith("/"))
        {
            file = pathname + "index.html";
        }
        file = file.TrimStart('/');
        if (!Exists(file))
        {
            context.Response.Redirect(Path.GetDirectoryName(pathname));
            return Task.CompletedTask;
        }
        if (IsDirectory(file))
        {
            context.Response.Redirect(pathname + "/");
            return Task.CompletedTask;
        }
        string extension = Path.GetExtension(file);
        string contentType;
        if (mimeTypeMap.TryGetValue(extension, out contentType))
        {
            byte[] buffer = CacheBuffer("default:" + file, () => File.ReadAllBytes(file));
            context.Response.ContentType = contentType;
            context.Response.ContentLength = buffer.Length;
            if (context.Request.Method != "HEAD")
            {
                return context.Response.Body.WriteAsync(buffer, 0, buffer.Length);
            }
            return Task.CompletedTask;
        }
        string data = CacheString("default:" + file, delegate() {
            string template = File.ReadAllText(file);
            var view = Merge(configuration);
            view["feed"] = (Func<string>) delegate() {
                string feed = (string) configuration["feed"];
                return (!string.IsNullOrEmpty(feed)) ? feed : context.Request.Scheme + "://" + context.Request.Host + "/atom.xml";
            };
            view["blog"] = (Func<string>) (() => RenderBlog(Posts(), 0));
            return Mustache(template, view, (name) => File.ReadAllText(name));
        });
        return WriteStringAsync(context, "text/html", data);
    }


    static class JsonReader
    {
        public static object Parse(string json)
        {
            JToken root = JToken.Parse(json);
            return Convert(root);
        }

        static object Convert(JToken token)
        {
            switch (token)
            {
                case JObject obj:
                    return new Dictionary<string, object>(obj.Properties().ToDictionary(pair => pair.Name, pair => Convert(pair.Value)));
                case JArray array:
                    return new List<object>(array.Select(item => Convert(item)));
                case JValue value:
                    return value.Value;
            }
            throw new NotSupportedException();
        }
    }

    class Router
    {
        List<Route> routes = new List<Route>();

        public Router(IDictionary<string, object> configuration)
        {
            if (configuration.ContainsKey("redirects"))
            {
                foreach (IDictionary<string, object> redirect in (IEnumerable<object>) configuration["redirects"])
                {
                    string target = (string) redirect["target"];
                    this.Get((string) redirect["pattern"], delegate(HttpContext context) {
                        context.Response.Redirect(target, true);
                        return Task.CompletedTask;
                    });
                }
            }
        }

        public void Get(string pattern, Func<HttpContext, Task> handler)
        {
            this.GetRoute(pattern).handlers["GET"] = handler;
        }

        Route GetRoute(string pattern)
        {
            Route route = this.routes.Find(item => item.Pattern == pattern);
            if (route == null)
            {
                route = new Route();
                route.Pattern = pattern;
                route.Regex = new Regex("^" + pattern.Replace("*", "(.*)") + "$");
                this.routes.Add(route);
            }
            return route;
        }

        public Task Handle(HttpContext context)
        {
            var pathname = context.Request.Path.Value;
            foreach (Route route in this.routes)
            {
                if (route.Regex.Match(pathname).Success)
                {
                    var method = context.Request.Method.ToUpper();
                    if (method == "HEAD" && !route.handlers.ContainsKey("HEAD"))
                    {
                        method = "GET";
                    }
                    Func<HttpContext, Task> handler = null;
                    if (route.handlers.TryGetValue(method, out handler))
                    {
                        return handler(context);
                    }
                }
            }
            return Task.CompletedTask;
        }

        class Route
        {
            public string Pattern;
            public Regex Regex;
            public Dictionary<string, Func<HttpContext, Task>> handlers = new Dictionary<string, Func<HttpContext, Task>>();
        }
    } 

    static void Main(string[] args)
    {
        string version = Path.GetFileName(AppContext.BaseDirectory).Replace("netcoreapp", string.Empty);
        Console.WriteLine("dotnetcore " + version);
        configuration = (IDictionary<string, object>) JsonReader.Parse(File.ReadAllText("app.json"));
        Router router = new Router(configuration);
        router.Get("/.git/?*", RootHandler);
        router.Get("/.vscode/?*", RootHandler);
        router.Get("/admin*", RootHandler);
        router.Get("/app.*", RootHandler);
        router.Get("/atom.xml", AtomHandler);
        router.Get("/header.html", RootHandler);
        router.Get("/meta.html", RootHandler);
        router.Get("/package.json", RootHandler);
        router.Get("/post.html", RootHandler);
        router.Get("/post.css", RootHandler);
        router.Get("/rss.xml", RssHandler);
        router.Get("/site.css", RootHandler);
        router.Get("/blog/*", PostHandler);
        router.Get("/blog", BlogHandler);
        router.Get("/.well-known/acme-challenge/*", CertHandler);
        router.Get("/*", DefaultHandler);
        int port = 8080;
        string url = "http://localhost:" + port.ToString();
        IWebHost host = new WebHostBuilder().UseKestrel().UseUrls(url)
            .Configure(app => { app.Run((context) => {
                    Console.WriteLine(context.Request.Method + " " + context.Request.Path.Value + context.Request.QueryString);
                    return router.Handle(context);
                });
            }).Build();
        environment = (IHostingEnvironment) host.Services.GetService(typeof(IHostingEnvironment));
        Console.WriteLine(environment.EnvironmentName.ToLower());
        InitPathCache(".");
        Console.WriteLine(url);
        host.Start();
        System.Threading.Thread.Sleep(Timeout.Infinite);
    }
}
