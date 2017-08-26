# Minimal

Minimal is a personal static website and blog generator for Go, Node.js and Python. It has no external dependencies and requires only a few hundred lines of code to run. Everything is minimal, easy to take appart and rewrite.

## Getting Started

To get started, [fork](https://help.github.com/articles/fork-a-repo) this repository and create a local [clone](https://help.github.com/articles/cloning-a-repository).

Modify `./content.json` to your liking (symbol codes for social links can be found [here](http://drinchev.github.io/monosocialiconsfont)). 

To build locally and launch a simple web server run **either** of the following: 

* Install [Node.js](https://nodejs.org/en/download) and run `./admin start node`.
* Install [Go](https://golang.org/doc/install) and run `./admin start go`.
* Install [Python](https://www.python.org/downloads/) and run `./admin start python`.

## Deployment

To deploy to a production enviroment set the `deployment` method in `./admin.cfg` and update the corresponding `.cfg` file in the `./deploy` folder, then run `./admin deploy` to initiate the build and deploy.