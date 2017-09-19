# Minimal

Minimal is a personal static website and blog generator for Go, Node.js and Python. It has no external dependencies and requires only a few hundred lines of code to run. Everything is minimal, easy to take appart and rewrite.

Example blogs are hosted on Netlify using the [default](https://minimal-default.netlify.com) and [profile](https://minimal-profile.netlify.com) themes.

## Getting Started

To get started, [fork](https://help.github.com/articles/fork-a-repo) this repository and create a local [clone](https://help.github.com/articles/cloning-a-repository).

Modify `./content.json` to your liking (symbol codes for social links can be found [here](http://drinchev.github.io/monosocialiconsfont)). 

To build locally and launch a simple web server run **either** of the following: 

* Install [Node.js](https://nodejs.org/en/download) and run `./task start node`.
* Install [Go](https://golang.org/doc/install) and run `./task start go`.
* Install [Python](https://www.python.org/downloads/) and run `./task start python`.

## Deployment

To deploy to a production enviroment set the `deployment` method in `./task.cfg` and update the corresponding `.cfg` file in the `./deploy` folder, then run `./task deploy` to initiate the build and deploy.

To host the repository on [Netlify](http://www.netlify.com) set the build command to `./task deploy netlify` and the publish directory to `build`.