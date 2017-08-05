# Minimal

Minimal is a personal website and blog template for Go, Node.js and Python. It has no external dependencies and requires only a few hundred lines of server code to run. Everything is minimal, easy to take appart and rewrite.

## Getting Started

To get started, [fork](https://help.github.com/articles/fork-a-repo) this repository and create a local [clone](https://help.github.com/articles/cloning-a-repository).

Modify `./app.json` to your liking (symbol codes for social links can be found [here](http://drinchev.github.io/monosocialiconsfont)). 

To launch the web server locally run **either** of the following: 

* Install [Node.js](https://nodejs.org/en/download) and run `node app.js`.
* Install [Go](https://golang.org/doc/install) and run `go run app.go`.
* Install [Python](https://www.python.org/downloads/) and run `python app.py`.

Finally, navigate to `http://localhost:8080` in your web browser to see the result.

## Admin Script

`./admin` is a [Bash](https://en.wikipedia.org/wiki/Bash_(Unix_shell)) script automating common tasks to run the website (on Windows use Git Bash or [WLS](https://en.wikipedia.org/wiki/Windows_Subsystem_for_Linux)). 

The script can be configured via `./admin.cfg` and provides two sets of commmands, one for local development and another for running the website on an actual Linux server.

For example, during local development  `./admin start` will launch the web server using the runtime configured via `./admin.cfg` and navigate to `http://localhost:8080` using the default web browser.

After cloning the repository at `/var/www/${site}` on an Ubuntu Linux production server run `./admin install` and `./admin start` to run the site as a [systemd](https://en.wikipedia.org/wiki/Systemd) service via an [NGINX](https://www.nginx.com) reverse proxy.
