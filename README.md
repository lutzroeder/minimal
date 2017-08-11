# Minimal

Minimal is a personal website generator and blog template for Go, Node.js and Python. It has no external dependencies and requires only a few hundred lines of code to run. Everything is minimal, easy to take appart and rewrite.

## Getting Started

To get started, [fork](https://help.github.com/articles/fork-a-repo) this repository and create a local [clone](https://help.github.com/articles/cloning-a-repository).

Modify `./app.json` to your liking (symbol codes for social links can be found [here](http://drinchev.github.io/monosocialiconsfont)). 

To build locally and launch a simple web server run **either** of the following: 

* Install [Node.js](https://nodejs.org/en/download) and run `./admin start node`.
* Install [Go](https://golang.org/doc/install) and run `./admin start go`.
* Install [Python](https://www.python.org/downloads/) and run `./admin start python`.

Finally, navigate to ``http://localhost:8080` in your web browser to see the result.

## Admin Script

`./admin` is a [Bash](https://en.wikipedia.org/wiki/Bash_(Unix_shell)) script automating common tasks to run the website (on Windows use Git Bash or [WLS](https://en.wikipedia.org/wiki/Windows_Subsystem_for_Linux)). 

The script can be configured via `./admin.cfg` and provides two sets of commmands, one for local development and another for running the website on an actual Linux server.

For example, during local development  `./admin start` will run the generator using the runtime configured via `./admin.cfg` and navigate to `http://localhost:8080` using the default web browser.

After cloning the repository on an Ubuntu Linux server modify `/etc/environment` to include `ENVIRONMENT=production` run `./admin install` and `./admin start` to run the site via an [NGINX](https://www.nginx.com).
