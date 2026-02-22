#!/usr/bin/env node

import * as fs from 'fs';
import * as path from 'path';
import * as process from 'process';

const environment = process.env.ENVIRONMENT;
console.log(`node ${process.version} ${environment}`);
const configuration = JSON.parse(fs.readFileSync("content.json", "utf-8"));
let destination = "build";
let theme = "default";
const args = process.argv.slice(2);
while (args.length > 0) {
    const arg = args.shift();
    if (arg === "--theme" && args.length > 0) {
        theme = args.shift();
    } else {
        destination = arg;
    }
}

const entityMap = {
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;", "/": "&#x2F;", "`": "&#x60;", "=": "&#x3D;"
};

const escapeHtml = (text) => {
    return text.replace(/[&<>"'`=/]/g, (char) => entityMap[char]);
};

const merge = (...args) => {
    let target = {};
    for (let i = 0; i < args.length; i++) {
        target = Object.assign(target, args[i]);
    }
    return target;
};

const mustache = (template, view, partials) => {
    template = template.replace(/{{#\s*([-_/.\w]+)\s*}}\s?([\s\S]*){{\/\1}}\s?/gm, (match, name, content) =>{
        if (name in view) {
            const section = view[name];
            if (Array.isArray(section) && section.length > 0) {
                return section.map((item) => mustache(content, merge(view, item), partials)).join("");
            }
            if ((typeof section === "boolean" || typeof section === 'string') && section) {
                return mustache(content, view, partials);
            }
        }
        return "";
    });
    template = template.replace(/{{>\s*([-_/.\w]+)\s*}}/gm, (match, name) => {
        return mustache(typeof partials === "function" ? partials(name) : partials[name], view, partials);
    });
    template = template.replace(/{{{\s*([-_/.\w]+)\s*}}}/gm, (match, name) => {
        if (name in view) {
            const value = view[name];
            return mustache(typeof value === "function" ? value() : value, view, partials);
        }
        return match;
    });
    template = template.replace(/{{\s*([-_/.\w]+)\s*}}/gm, (match, name) => {
        if (name in view) {
            const value = view[name];
            return escapeHtml(typeof value === "function" ? value() : value);
        }
        return match;
    });
    return template;
};

const formatDate = (date, format) => {
    switch (format) {
        case "atom": {
            return date.toISOString().replace(/\.[0-9]*Z/, "Z");
        }
        case "rss": {
            return date.toUTCString().replace(" GMT", " +0000");
        }
        case "user": {
            const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
            return `${months[date.getMonth()]} ${date.getDate()}, ${date.getFullYear()}`;
        }
        default: {
            throw new Error(`Unsupported date format: ${format}`);
        }
    }
};

const truncateMap = { "pre": true, "code": true, "img": true, "table": true, "style": true, "script": true, "h2": true, "h3": true };

const truncate = (text, length) => {
    const closeTags = new Map();
    let ellipsis = "";
    let count = 0;
    let index = 0;
    while (count < length && index < text.length) {
        if (text[index] === '<') {
            if (closeTags.has(index)) {
                const closeTag = closeTags.get(index);
                closeTags.delete(index);
                index += closeTag.length;
            } else {
                const match = text.substring(index).match("<(\\w+)[^>]*>");
                if (match) {
                    const tag = match[1].toLowerCase();
                    if (tag in truncateMap) {
                        break;
                    }
                    index += match[0].length;
                    const closeTagRegExp = new RegExp(`(</${tag}\\s*>)`, "i");
                    const end = text.substring(index).search(closeTagRegExp);
                    if (end !== -1) {
                        closeTags.set(index + end, `</${tag}>`);
                    }
                } else {
                    index++;
                    count++;
                }
            }
        } else if (text[index] === "&") {
            index++;
            const entity = text.substring(index).match("(#?[A-Za-z0-9]+;)");
            if (entity) {
                index += entity[0].length;
            }
            count++;
        } else {
            if (text[index] === " ") {
                index++;
                count++;
            }
            let skip = text.substring(index).search(" |<|&");
            if (skip === -1) {
                skip = text.length - index;
            }
            if (count + skip >= length) {
                ellipsis = "&hellip;";
            }
            if (count + skip - 15 > length) {
                skip = length - count;
            }
            index += skip;
            count += skip;
        }
    }
    const output = [text.substring(0, index)];
    if (ellipsis !== "") {
        output.push(ellipsis);
    }
    const keys = [...closeTags.keys()];
    for (const key of keys.map((key) => Number(key)).sort()) {
        output.push(closeTags.get(key));
    }
    return output.join("");
};

const htmlBlockTags = ['style', 'script', 'svg', 'p'];

const markdown = (text) => {
    const lines = text.split(/\n/);
    const output = [];
    let i = 0;
    let inHTML = '';
    while (i < lines.length) {
        const line = lines[i];
        if (inHTML) {
            output.push(line);
            if (line.includes(`</${inHTML}>`) || line.includes(`<${inHTML}/>`) || line.includes(`<${inHTML} />`)) {
                inHTML = '';
            }
            i++;
            continue;
        }
        if (line.startsWith('```')) {
            const code = [];
            i++;
            while (i < lines.length && !lines[i].startsWith('```')) {
                code.push(lines[i]);
                i++;
            }
            i++;
            output.push(`<pre>${code.join('\n').replace(/\*\*(.+?)\*\*/g, '<b>$1</b>')}</pre>`);
        } else if (line.startsWith('#')) {
            const match = line.match(/^(#{1,6})\s+(.*)/);
            if (match) {
                const level = match[1].length;
                output.push(`<h${level}>${inline(match[2])}</h${level}>`);
            }
            i++;
        } else if (line.trimStart().startsWith('<')) {
            for (const tag of htmlBlockTags) {
                if (line.trimStart().toLowerCase().startsWith(`<${tag}`)) {
                    if (!line.includes(`</${tag}>`) && !line.includes(`<${tag}/>`) && !line.includes(`<${tag} />`)) {
                        inHTML = tag;
                    }
                    break;
                }
            }
            output.push(line);
            i++;
        } else if (line.trim() === '') {
            i++;
        } else {
            const block = [];
            while (i < lines.length && lines[i].trim() !== '' && !lines[i].startsWith('#') && !lines[i].startsWith('```') && !lines[i].trimStart().startsWith('<')) {
                block.push(lines[i]);
                i++;
            }
            output.push(`<p>${inline(block.join('\n'))}</p>`);
        }
    }
    function inline(text) {
        text = text.replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<img alt="$1" src="$2">');
        text = text.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2">$1</a>');
        text = text.replace(/`([^`]+)`/g, '<code>$1</code>');
        text = text.replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>');
        text = text.replace(/\*([^*]+)\*/g, '<i>$1</i>');
        return text;
    }
    return output.join('\n');
};

const loadPost = (file) => {
    if (fs.existsSync(file) && !fs.statSync(file).isDirectory()) {
        const data = fs.readFileSync(file, "utf-8");
        if (data) {
            const item = {};
            const content = [];
            let metadata = -1;
            const lines = data.split(/\r\n?|\n/g);
            while (lines.length > 0) {
                const line = lines.shift();
                if (line.startsWith("---")) {
                    metadata++;
                } else if (metadata === 0) {
                    const index = line.indexOf(":");
                    if (index >= 0) {
                        const name = line.slice(0, index).trim();
                        let value = line.slice(index + 1).trim();
                        if (value.startsWith('"') && value.endsWith('"')) {
                            value = value.slice(1, -1);
                        }
                        item[name] = value;
                    }
                } else {
                    content.push(line);
                }
            }
            item.content = content.join("\n");
            if (file.endsWith('.md')) {
                item.content = markdown(item.content);
            }
            return item;
        }
    }
    return null;
};

const posts = () => {
    const files = fs.readdirSync("content/blog/");
    return files.filter((post) => fs.statSync(`content/blog/${post}`).isDirectory() && fs.existsSync(`content/blog/${post}/index.md`)).sort().reverse();
};

const renderBlog = (folders, destination, root, page) => {
    const view = { "items": [] };
    let count = 10;
    while (count > 0 && folders.length > 0) {
        const folder = folders.shift();
        const item = loadPost(`content/blog/${folder}/index.md`);
        if (item && (item.state === "post" || environment !== "production")) {
            item.url = `blog/${folder}/`;
            if ("date" in item) {
                const date = new Date(`${item.date.split(/ \+| -/)[0]}Z`);
                item.date = formatDate(date, "user");
            }
            const content = item.content.replace(/\s\s/g, " ");
            const truncated = truncate(content, 250);
            item.content = truncated;
            item.more = truncated !== content;
            view.items.push(item);
            count--;
        }
    }
    view.placeholder = [];
    view.root = root;
    if (folders.length > 0) {
        page++;
        const location = `blog/page${page.toString()}.html`;
        view.placeholder.push({ "url": `${root}../${location}` });
        const file = `${destination}/${location}`;
        const data = renderBlog(folders, destination, root, page);
        fs.writeFileSync(file, data);
    }
    const template = fs.readFileSync(`themes/${theme}/feed.html`, "utf-8");
    return mustache(template, view, null);
};

const renderFeed = (source, destination) => {
    const host = configuration.host;
    const format = path.extname(source).replace(".", "");
    let count = 10;
    const feed = {
        name: configuration.name,
        description: configuration.description,
        author: configuration.name,
        url: configuration.feeds[0].url,
        host,
        items: []
    };
    const folders = posts();
    let recentFound = false;
    let recent = new Date();
    while (folders.length > 0 && count > 0) {
        const folder = folders.shift();
        const item = loadPost(`content/blog/${folder}/index.md`);
        if (item && (item.state === "post" || environment !== "production")) {
            item.url = `${host}/blog/${folder}/`;
            if (!item.author || item.author === configuration.name) {
                item.author = false;
            }
            if ("date" in item) {
                const date = new Date(item.date);
                let updated = date;
                if ("updated" in item) {
                    updated = new Date(item.updated);
                }
                item.date = formatDate(date, format);
                item.updated = formatDate(updated, format);
                if (!recentFound || recent < updated) {
                    recent = updated;
                    recentFound = true;
                }
            }
            item.content = escapeHtml(item.content);
            feed.items.push(item);
            count--;
        }
    }
    feed.updated = formatDate(recent, format);
    const template = fs.readFileSync(source, "utf-8");
    const data = mustache(template, feed, null);
    fs.writeFileSync(destination, data);
};

const renderPost = (source, destination, root) => {
    if (source.startsWith("content/blog/") && source.endsWith("/index.md")) {
        const item = loadPost(source);
        if (item) {
            if (item.updated && item.updated !== item.date) {
                const date = new Date(`${item.updated.split(/ \+| -/)[0]}Z`);
                item.updated = formatDate(date, "user");
            } else {
                delete item.updated;
            }
            if (item.date) {
                const date = new Date(`${item.date.split(/ \+| -/)[0]}Z`);
                item.date = formatDate(date, "user");
            }
            item.author = item.author || configuration.name;
            const view = merge(configuration, item);
            view.root = root;
            const template = fs.readFileSync(`themes/${theme}/post.html`, "utf-8");
            const data = mustache(template, view, (name) => {
                return fs.readFileSync(`themes/${theme}/${name}`, "utf-8");
            });
            fs.writeFileSync(destination, data);
            return true;
        }
    }
    return false;
};

const renderPage = (source, destination, root) =>{
    if (renderPost(source, destination, root)) {
        return;
    }
    const template = fs.readFileSync(source, "utf-8");
    const view = merge(configuration);
    view.root = root;
    const next = `<script type="text/javascript">
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
`;
    view.blog = function() {
        const content = renderBlog(posts(), path.dirname(destination), root, 0);
        return `${content}${next}`;
    };
    view.pages = [];
    configuration.pages.forEach((page) => {
        const location = path.dirname(source);
        const target = mustache(page.url, view);
        const active = path.join(location, target) === location;
        if (active || page.visible) {
            view.pages.push({ name: page.name, url: page.url, active });
        }
    });
    const data = mustache(template, view, (name) => {
        return fs.readFileSync(`themes/${theme}/${name}`, "utf-8");
    });
    fs.writeFileSync(destination, data);
};

const renderFile = (source, destination) => {
    fs.createReadStream(source).pipe(fs.createWriteStream(destination));
};

const render = (source, destination, root) => {
    console.log(destination);
    const extension = path.extname(source);
    switch (extension) {
        case ".rss":
        case ".atom":
            renderFeed(source, destination);
            break;
        case ".html":
        case ".md":
            renderPage(source, destination, root);
            break;
        default:
            renderFile(source, destination);
            break;
    }
};

const makeDirectory = (directory) =>{
    directory.split("/").reduce((current, folder) => {
        current += `${folder}/`;
        if (!fs.existsSync(current)) {
            fs.mkdirSync(current);
        }
        return current;
    }, "");
};

const renderDirectory = (source, destination, root) => {
    makeDirectory(destination);
    const items = fs.readdirSync(source);
    for (const item of items) {
        if (!item.startsWith(".")) {
            if (fs.statSync(source + item).isDirectory()) {
                renderDirectory(`${source}${item}/`, `${destination}${item}/`, `${root}../`);
            } else {
                const dest = item.endsWith('.md') ? destination + item.replace(/\.md$/, '.html') : destination + item;
                render(source + item, dest, root);
            }
        }
    }
};

const cleanDirectory = (directory) => {
    if (fs.existsSync(directory) && fs.statSync(directory).isDirectory()) {
        fs.readdirSync(directory).forEach((item) => {
            item = `${directory}/${item}`;
            if (fs.statSync(item).isDirectory()) {
                cleanDirectory(item);
                fs.rmdirSync(item);
            } else {
                fs.unlinkSync(item);
            }
        });
    }
};

cleanDirectory(destination);
renderDirectory("content/", `${destination}/`, "");
