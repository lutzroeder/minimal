using System;
using System.Collections.Generic;
using System.IO;
using System.Linq;
using System.Net;
using System.Reflection;
using System.Text;
using System.Text.RegularExpressions;
using System.Threading;
using System.Threading.Tasks;
using Microsoft.AspNetCore.Builder;
using Microsoft.AspNetCore.Hosting;
using Microsoft.AspNetCore.Http;
using Microsoft.Extensions.DependencyInjection;
using Newtonsoft.Json.Linq;

class Program
{
    static IHostingEnvironment environment;
    static IDictionary<string, object> configuration;

    static Dictionary<string,string> entityMap = new Dictionary<string,string>() {
        { "&", "&amp;" }, { "<", "&lt;" }, { ">", "&gt;" }, { "\"", "&quot;" },
        { "'", "&#39;" }, { "/", "&#x2F;" }, { "`", "&#x60;" }, { "=", "&#x3D;" }
    };
    static Regex entityRegex = new Regex("[&<>\"\'`=\\/]");

    static string EscapeHtml(string text)
    {
        return entityRegex.Replace(text, delegate(Match match) {
            return entityMap[match.Groups[0].Value];
        });
    }

    static Regex partialRegex = new Regex(@"\{\{>\s*([-_/.\w]+)\s*\}\}");
    static Regex replaceRegex = new Regex(@"{{{\s*([-_/.\w]+)\s*}}}");
    static Regex escapeRegex = new Regex(@"{{\s*([-_/.\w]+)\s*}}");

    static string Mustache(string template, Dictionary<string, object> view, Func<string, string> partials)
    {
        template = partialRegex.Replace(template, delegate(Match match) {
            return Mustache(partials(match.Groups[1].Value), view, partials);
        });
        template = replaceRegex.Replace(template, delegate(Match match) {
            object value;
            if (view.TryGetValue(match.Groups[1].Value, out value))
            {
                if (value is Func<string>)
                {
                    return ((Func<string>)value)();
                }
                if (value is string)
                {
                    return (string) value;
                }
            }
            return match.Groups[0].Value;
        });
        template = escapeRegex.Replace(template, delegate(Match match) {
            object value;
            if (view.TryGetValue(match.Groups[1].Value, out value))
            {
                if (value is Func<string>)
                {
                    return EscapeHtml(((Func<string>)value)());
                }
                if (value is string)
                {
                    return EscapeHtml((string) value);
                }
            }
            return match.Groups[0].Value;
        });

        return template;
    }

    static string FormatDate(DateTime date)
    {
        return date.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ");
    }

    static string FormatUserDate(string text)
    {
        DateTime dateTime;
        if (DateTime.TryParse(text, out dateTime))
        {
            return dateTime.ToString("MMM d, yyyy");
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
        return (string) Cache(key, delegate() {
            return (string) callback();
        });
    }

    static byte[] CacheBuffer(string key, Func<byte[]> callback)
    {
        return (byte[]) Cache(key, delegate() {
            return (byte[]) callback();
        });
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
        "pre", "code", "img", "table", "style", "script" 
    };

    static string Truncate(string text, int length)
    {
        var closeTags = new SortedDictionary<int, string>();
        var ellipsis = "";
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
        return string.Join("", output);
    }

    static Dictionary<string,string> LoadPost(string file)
    {
        if (Exists(file) && !IsDirectory(file))
        {
            var data = File.ReadAllText(file);
            var lines = new Queue<string>(new Regex("\\r\\n?|\\n").Split(data));
            var entry = new Dictionary<string, string>();
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
                        entry[name] = value;
                    }
                }
                else
                {
                    content.Add(line);
                }
            }
            entry["content"] = string.Join("\n", content);
            return entry;
        }
        return null;
    }

    static Queue<string> Posts()
    {
        var posts = (List<string>) Cache("blog:files", delegate () {
            var list = new List<string>(Directory.GetFileSystemEntries("blog/", "*.html").Select(file => Path.GetFileName(file)));
            list.Sort();
            list.Reverse();
            return list;
        });
        return new Queue<string>(posts);
    }

    static string RenderBlog(Queue<string> files, int start)
    {
        var output = new List<string>();
        int length = 10;
        int index = 0;
        while (files.Count > 0 && index < (start + length))
        {
            string file = files.Dequeue();
            var entry = LoadPost("blog/" + file);
            if (entry != null && (entry["state"] == "post" || !environment.IsProduction()))
            {
                if (index >= start)
                {
                    var location = "/blog/" + Path.GetFileNameWithoutExtension(file);
                    entry["date"] = FormatUserDate(entry["date"]);
                    var post = new List<string>();
                    post.Add("<div class='item'>");
                    post.Add("<div class='date'>" + entry["date"] + "</div>");
                    post.Add("<h1><a href='" + location + "'>" + entry["title"] + "</a></h1>");
                    post.Add("<div class='content'>");
                    var content = entry["content"];
                    content = new Regex("\\s\\s").Replace(content, " ");
                    var truncated = Truncate(content, 250);
                    post.Add(truncated);
                    post.Add("</div>");
                    if (truncated != content)
                    {
                        post.Add("<div class='more'><a href='" + location + "'>" + "Read more&hellip;" + "</a></div>");
                    }
                    post.Add("</div>");
                    output.Add(string.Join("\n", post) + "\n");
                }
                index++;
            }

        }
        if (files.Count > 0)
        {
            var template = File.ReadAllText("stream.html");
            var view = new Dictionary<string, object>() { { "url", "/blog?id=" + index.ToString() } };
            var data = Mustache(template, view, null);
            output.Add(data);
        }
        return string.Join("\n", output);
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
        var data = CacheString("atom:" + host + "/blog/atom.xml", delegate () {
            var count = 10;
            var output = new List<string>();
            output.Add("<?xml version='1.0' encoding='UTF-8'?>");
            output.Add("<feed xmlns='http://www.w3.org/2005/Atom'>");
            output.Add("<title>" + configuration["name"] + "</title>");
            output.Add("<id>" + host + "/</id>");
            output.Add("<icon>" + host + "/favicon.ico</icon>");
            var index = output.Count;
            string recent = string.Empty;
            output.Add("");
            output.Add("<author><name>" + configuration["name"] + "</name></author>");
            output.Add("<link rel='alternate' type='text/html' href='" + host + "/' />");
            output.Add("<link rel='self' type='application/atom+xml' href='" + host + "/blog/atom.xml' />");
            var files = Posts();
            while (files.Count > 0 && count > 0) 
            {
                string file = files.Dequeue();
                var entry = LoadPost("blog/" + file);
                if (entry != null && (entry["state"] == "post" || !environment.IsProduction()))
                {
                    var url = host + "/blog/" + Path.GetFileNameWithoutExtension(file);
                    output.Add("<entry>");
                    output.Add("<id>" + url + "</id>");
                    if (entry.ContainsKey("author") && (entry["author"] != ((string) configuration["name"]))) 
                    {
                        output.Add("<author><name>" + entry["author"] + "</name></author>");
                    }
                    var date = FormatDate(DateTime.Parse(entry["date"]));
                    output.Add("<published>" + date + "</published>");
                    var updated = entry.ContainsKey("updated") ? FormatDate(DateTime.Parse(entry["updated"])) : date;
                    output.Add("<updated>" + updated + "</updated>");
                    if (string.IsNullOrEmpty(recent) || recent.CompareTo(updated) < 0)  {
                        recent = updated;
                    }
                    output.Add("<title type='text'>" + entry["title"] + "</title>");
                    var content = EscapeHtml(Truncate(entry["content"], 10000));
                    output.Add("<content type='html'>" + content + "</content>");
                    output.Add("<link rel='alternate' type='text/html' href='" + url + "' title='" + entry["title"] + "' />");
                    output.Add("</entry>");
                    count--;
                }
            }
            recent = !string.IsNullOrEmpty(recent) ? recent : FormatDate(DateTime.Now);
            output[index] = "<updated>" + recent + "</updated>";
            output.Add("</feed>");
            return string.Join("\n", output);
        });
        return WriteStringAsync(context, "application/atom+xml", data);
    }

    static Dictionary<string,string> mimeTypeMap = new Dictionary<string,string>() {
        { ".js", "text/javascript" },
        { ".css", "text/css" },
        { ".png", "image/png" },
        { ".gif", "image/gif" },
        { ".jpg", "image/jpeg" },
        { ".ico", "image/x-icon" },
        { ".zip", "application/zip" },
        { ".json", "application/json" },
    };

    static Task PostHandler(HttpContext context)
    {
        var pathname = context.Request.Path.Value;
        var file = pathname.TrimStart('/');
        string data = CacheString("post:" + file, delegate() {
            var entry = LoadPost(file + ".html");
            if (entry != null)
            {
                entry["date"] = FormatUserDate(entry["date"]);
                if (!entry.ContainsKey("author"))
                {
                    entry["author"] = (string) configuration["name"];
                }
                var view = new Dictionary<string, object>();
                foreach (KeyValuePair<string, object> pair in configuration)
                {
                    view[pair.Key] = pair.Value;
                }
                foreach (KeyValuePair<string, string> pair in entry)
                {
                    view[pair.Key] = pair.Value;
                }
                var template = File.ReadAllText("post.html");
                return Mustache(template, view, delegate(string name) {
                    return File.ReadAllText(name);
                });
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
                var data = "";
                if (id < files.Count)
                {
                    data = CacheString("blog:" + key, delegate() {
                        return RenderBlog(files, id);
                    });
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
                var data = File.ReadAllText(file);;
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
            byte[] buffer = CacheBuffer("default:" + file, delegate() {
                return File.ReadAllBytes(file);
            });
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
            var view = new Dictionary<string, object>();
            foreach (KeyValuePair<string, object> pair in configuration)
            {
                view[pair.Key] = pair.Value;
            }
            view["feed"] = (Func<string>) delegate() {
                string feed = (string) configuration["feed"];
                return (!string.IsNullOrEmpty(feed)) ? feed : context.Request.Scheme + "://" + context.Request.Host + "/blog/atom.xml";
            };
            view["links"] = (Func<string>) delegate() {
                return string.Join("\n", ((IEnumerable<object>) configuration["links"]).Select(delegate(object obj) {
                    IDictionary<string, object> link = (IDictionary<string, object>) obj;
                    return "<a class='icon' target='_blank' href='" + link["url"] + "' title='" + link["name"] + "'><span class='symbol'>" + link["symbol"] + "</span></a>";
                }));
            };
            view["tabs"] = (Func<string>) delegate() {
                return string.Join("\n", ((IEnumerable<object>) configuration["pages"]).Select(delegate(object obj) {
                    IDictionary<string, object> page = (IDictionary<string, object>) obj;
                    return "<li class='tab'><a href='" + page["url"] + "'>" + page["name"] + "</a></li>";
                }));
            };
            view["blog"] = (Func<string>) delegate() {
                return RenderBlog(Posts(), 0);
            };
            return Mustache(template, view, delegate (string name) {
                return File.ReadAllText(name);
            });
        });
        return WriteStringAsync(context, "text/html", data);
    }


    static class JsonReader
    {
        public static object Parse(string json)
        {
            JObject root = JObject.Parse(json);
            return Convert(root);
        }

        static object Convert(JToken token)
        {
            switch (token)
            {
                case JObject obj:
                    Dictionary<string, object> dictionary = new Dictionary<string, object>();
                    foreach (JProperty property in obj.Properties())
                    {
                        dictionary[property.Name] = Convert(property.Value);
                    }
                    return dictionary;
                case JArray array:
                    List<object> list = new List<object>();
                    foreach (JToken item in array)
                    {
                        list.Add(Convert(item));
                    }
                    return list;
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
                    this.Get((string) redirect["pattern"], delegate (HttpContext context) {
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

        private Route GetRoute(string pattern)
        {
            Route route = this.routes.Find(delegate(Route item) {
                return item.Pattern == pattern;
            });
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
        string version = Path.GetFileName(Path.GetDirectoryName(Assembly.GetEntryAssembly().Location)).Replace("netcoreapp", "");
        Console.WriteLine("dotnetcore " + version);
        configuration = (IDictionary<string, object>) JsonReader.Parse(File.ReadAllText("app.json"));
        Router router = new Router(configuration);
        router.Get("/.git/?*", RootHandler);
        router.Get("/.vscode/?*", RootHandler);
        router.Get("/admin*", RootHandler);
        router.Get("/app.*", RootHandler);
        router.Get("/header.html", RootHandler);
        router.Get("/meta.html", RootHandler);
        router.Get("/package.json", RootHandler);
        router.Get("/post.html", RootHandler);
        router.Get("/post.css", RootHandler);
        router.Get("/site.css", RootHandler);
        router.Get("/blog/atom.xml", AtomHandler);
        router.Get("/blog/*", PostHandler);
        router.Get("/blog", BlogHandler);
        router.Get("/.well-known/acme-challenge/*", CertHandler);
        router.Get("/*", DefaultHandler);
        int port = 8080;
        string url = "http://localhost:" + port.ToString();
        IWebHostBuilder webHostBuilder = new WebHostBuilder().UseKestrel()
            .UseSetting(WebHostDefaults.ServerUrlsKey, url)
            .Configure(delegate(IApplicationBuilder applicationBuilder) {
                applicationBuilder.Run(delegate(HttpContext context){
                    Console.WriteLine(context.Request.Method + " " + context.Request.Path.Value);
                    return router.Handle(context);
                });
            }); 
        IWebHost webHost = webHostBuilder.Build();
        environment = webHost.Services.GetRequiredService<IHostingEnvironment>();
        Console.WriteLine(environment.EnvironmentName.ToLower());
        InitPathCache(".");
        Console.WriteLine(url);
        webHost.Start();
        System.Threading.Thread.Sleep(Timeout.Infinite);
    }
}
